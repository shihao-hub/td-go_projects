package engine

// 决策流：
//
//	目标元数据索引（纯 stat，无 I/O）      —— 秒级
//	 └─ 源目录流式扫描，逐文件三级判定：
//	     1) 目标缺失            → 复制
//	     2) size 不同           → 复制
//	     3) size 同、mtime 同   → 跳过（零文件读取）
//	     4) size 同、mtime 不同 → 双侧哈希（缓存感知）比对
//	        - 内容不同 → 复制
//	        - 内容相同 → 修正目标 mtime，下次走 3)
//	     复制动作即时进队列，worker 池并发消化（扫描与复制重叠）
//	 └─ 扫描结束 → 未被源命中的目标文件 = 待删除清单 → 用户确认 → 删除
//
// 强制模式绕过 3) 的快速判定与哈希缓存读取：全量内容校验。

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"file-sync-native/ignore"
	"file-sync-native/models"
)

const copyWorkers = 3 // 并发复制数：SSD 上有收益，HDD 上再多反而抖

// Options 控制一次同步运行的行为。
type Options struct {
	Force            bool // 强制模式：全量内容校验，绕过缓存读取
	DryRun           bool // 只算差异不落盘（diff 预览）
	OnPendingDeletes func(deletes []string) bool // 返回 true 才执行删除
}

// Result 汇总一次运行的统计。
type Result struct {
	Copied          int
	Skipped         int
	Deleted         int
	DeletedDeclined int // 用户拒绝删除的条数
	Errors          []string
	Diff            models.FileDiff // DryRun 下为完整差异；正式运行同样填充
}

type copyAction struct {
	rel  string
	size int64
}

// Run 执行一次同步。致命错误（源目录无效等）通过 error 返回，
// 单文件错误记录在 Result.Errors 且不中断整体。
func Run(ctx context.Context, task *models.SyncTask, cache *HashCache, tracker *Tracker, opts Options) (*Result, error) {
	res := &Result{Diff: models.FileDiff{Added: []string{}, Modified: []string{}, Deleted: []string{}}}
	if tracker == nil {
		tracker = NewTracker(task.ID)
	}
	m := ignore.NewMatcher(task.IgnoreRules)

	defer func() {
		if cache != nil {
			if err := cache.Save(); err != nil {
				res.Errors = append(res.Errors, "保存哈希缓存失败: "+err.Error())
			}
		}
	}()

	if fi, err := os.Stat(task.SourcePath); err != nil || !fi.IsDir() {
		msg := "源目录不存在或不是目录"
		tracker.Fail(msg)
		return res, errors.New(msg)
	}
	if fi, err := os.Stat(task.TargetPath); err == nil && !fi.IsDir() {
		msg := "目标路径已存在且不是目录"
		tracker.Fail(msg)
		return res, errors.New(msg)
	}

	// 1. 目标元数据索引：纯 stat，无文件内容读取。
	tgtIndex, tgtErrs := indexTarget(task.TargetPath, m)
	for _, e := range tgtErrs {
		res.Errors = append(res.Errors, "目标扫描: "+e)
	}

	// 2. 复制通道 + worker 池（DryRun 时不启动）。
	var (
		actions chan copyAction
		wg      sync.WaitGroup
		copyErrMu sync.Mutex
	)
	if !opts.DryRun {
		actions = make(chan copyAction, 64)
		for w := 0; w < copyWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for a := range actions {
					if ctx.Err() != nil {
						continue // 排空队列，避免发送端阻塞
					}
					src := filepath.Join(task.SourcePath, filepath.FromSlash(a.rel))
					dst := filepath.Join(task.TargetPath, filepath.FromSlash(a.rel))
					if err := copyFile(src, dst); err != nil {
						copyErrMu.Lock()
						res.Errors = append(res.Errors, fmt.Sprintf("复制 %s: %v", a.rel, err))
						copyErrMu.Unlock()
					}
					tracker.FileDone(a.size, a.rel)
				}
			}()
		}
	}

	// 3. 源目录流式扫描 + 逐文件决策。
	seen := make(map[string]bool)
	enqueued := 0
	scanErr := filepath.WalkDir(task.SourcePath, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(task.SourcePath, p)
		if rerr != nil || rel == "." {
			if err != nil && rerr == nil {
				return err
			}
			return nil
		}
		if err != nil {
			res.Errors = append(res.Errors, "源扫描: "+p+": "+err.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if m.Match(filepath.ToSlash(rel), true) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			res.Errors = append(res.Errors, "跳过符号链接: "+p)
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if m.Match(relSlash, false) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			res.Errors = append(res.Errors, "源扫描: "+p+": "+ierr.Error())
			return nil
		}
		tracker.ScanTick(relSlash)
		seen[relSlash] = true

		tf, dstOK := tgtIndex[relSlash]
		need, kind, derr := decide(ctx, decideInput{
			srcAbs: p,
			dstAbs: filepath.Join(task.TargetPath, filepath.FromSlash(relSlash)),
			rel:    relSlash,
			size:   info.Size(),
			mtime:  info.ModTime(),
			dst:    tf,
			dstOK:  dstOK,
			force:  opts.Force,
			dryRun: opts.DryRun,
			cache:  cache,
		})
		if derr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("比对 %s: %v", relSlash, derr))
		}
		if !need {
			res.Skipped++
			return nil
		}
		if kind == "added" {
			res.Diff.Added = append(res.Diff.Added, relSlash)
		} else {
			res.Diff.Modified = append(res.Diff.Modified, relSlash)
		}
		if opts.DryRun {
			return nil
		}
		tracker.CopyEnqueued(info.Size())
		select {
		case actions <- copyAction{rel: relSlash, size: info.Size()}:
			enqueued++
		case <-ctx.Done():
			return filepath.SkipAll
		}
		return nil
	})
	if scanErr != nil && !errors.Is(scanErr, filepath.SkipAll) {
		res.Errors = append(res.Errors, "源扫描中断: "+scanErr.Error())
	}
	if actions != nil {
		close(actions)
	}
	if enqueued > 0 {
		tracker.CopyPhase()
	}
	wg.Wait()

	if ctx.Err() != nil {
		tracker.Cancelled()
		return res, context.Canceled
	}

	// 4. 删除清单 = 目标索引中未被源命中的文件。
	var deletes []string
	for rel := range tgtIndex {
		if !seen[rel] {
			deletes = append(deletes, rel)
		}
	}
	sort.Strings(deletes)
	res.Diff.Deleted = deletes

	if opts.DryRun {
		sort.Strings(res.Diff.Added)
		sort.Strings(res.Diff.Modified)
		tracker.Complete()
		return res, nil
	}

	// 5. 删除需用户确认。
	if len(deletes) > 0 && opts.OnPendingDeletes != nil {
		tracker.AwaitingDelete(deletes)
		if opts.OnPendingDeletes(deletes) {
			if errs := deleteFiles(ctx, task.TargetPath, deletes, tracker.DeleteTick); len(errs) > 0 {
				for _, e := range errs {
					res.Errors = append(res.Errors, e.Error())
				}
			} else {
				res.Deleted = len(deletes)
			}
			if ctx.Err() == nil {
				removeEmptyDirs(task.TargetPath)
			}
		} else {
			res.DeletedDeclined = len(deletes)
		}
	} else if len(deletes) > 0 {
		// 没有确认回调（如脱离 UI 调用）：保守起见不删。
		res.DeletedDeclined = len(deletes)
	}

	if ctx.Err() != nil {
		tracker.Cancelled()
		return res, context.Canceled
	}

	res.Copied = enqueued
	sort.Strings(res.Diff.Added)
	sort.Strings(res.Diff.Modified)
	if len(res.Errors) > 0 {
		tracker.Fail(fmt.Sprintf("%d 项操作失败，首个错误: %s", len(res.Errors), res.Errors[0]))
	} else {
		tracker.Complete()
	}
	return res, nil
}

// indexTarget 收集目标侧文件元数据（不读内容、不算哈希）。
func indexTarget(root string, m *ignore.Matcher) (map[string]models.FileInfo, []string) {
	index := make(map[string]models.FileInfo)
	var errs []string
	if fi, err := os.Stat(root); err != nil {
		return index, nil // 目标不存在：全部视为新增
	} else if !fi.IsDir() {
		return index, []string{"目标路径不是目录"}
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		if err != nil {
			errs = append(errs, p+": "+err.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if m.Match(filepath.ToSlash(rel), true) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if m.Match(relSlash, false) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			errs = append(errs, p+": "+ierr.Error())
			return nil
		}
		index[relSlash] = models.FileInfo{
			Path:    relSlash,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   false,
		}
		return nil
	})
	return index, errs
}

type decideInput struct {
	srcAbs string
	dstAbs string
	rel    string
	size   int64
	mtime  time.Time
	dst    models.FileInfo
	dstOK  bool
	force  bool
	dryRun bool
	cache  *HashCache
}

// decide 对单个文件给出"是否需要复制"的判定。
func decide(ctx context.Context, in decideInput) (need bool, kind string, err error) {
	if !in.dstOK {
		return true, "added", nil
	}
	if in.force {
		sh, err := hashWith(in.cache, in.srcAbs, in.mtime, in.size, true)
		if err != nil {
			return false, "", err
		}
		dh, err := hashWith(in.cache, in.dstAbs, in.dst.ModTime, in.dst.Size, true)
		if err != nil {
			return false, "", err
		}
		return sh != dh, "modified", nil
	}
	if in.size != in.dst.Size {
		return true, "modified", nil
	}
	if in.mtime.Equal(in.dst.ModTime) {
		return false, "", nil // 快速判定：零文件读取
	}
	sh, err := hashWith(in.cache, in.srcAbs, in.mtime, in.size, false)
	if err != nil {
		return false, "", err
	}
	dh, err := hashWith(in.cache, in.dstAbs, in.dst.ModTime, in.dst.Size, false)
	if err != nil {
		return false, "", err
	}
	if sh == dh {
		// 内容相同仅时间不同：把目标 mtime 修正为源，下次走快速判定。
		if !in.dryRun {
			_ = os.Chtimes(in.dstAbs, time.Now(), in.mtime)
		}
		return false, "", nil
	}
	return true, "modified", nil
}

func hashWith(c *HashCache, abs string, mt time.Time, size int64, bypass bool) (string, error) {
	if c == nil {
		return computeHash(abs)
	}
	return c.HashOf(abs, mt, size, bypass)
}
