package engine

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"

	"file-sync-native/models"
)

func walkFiles(root string, fn func(abs string)) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fn(p)
		return nil
	})
}

// 基准树：2000 个 16KB 文件，分散在 20 个目录，约 31 MiB。
// 对比两种决策成本：
//   新引擎稳态：目标元数据索引 + size/mtime 快速判定（真实 Run 调用）
//   旧版成本模型：双侧全量 SHA256（旧 file-sync 每次 ComputeDiff 的做法）
const (
	benchDirs     = 20
	benchFilesPer = 100
	benchFileSize = 16 * 1024
)

func buildBenchTree(b *testing.B, root string) {
	b.Helper()
	chunk := make([]byte, benchFileSize)
	for i := range chunk {
		chunk[i] = byte(i*31 + 7)
	}
	for d := 0; d < benchDirs; d++ {
		for f := 0; f < benchFilesPer; f++ {
			p := filepath.Join(root, fmt.Sprintf("d%02d", d), fmt.Sprintf("f%04d.bin", f))
			mustWrite(nil, p, chunk)
		}
	}
}

func benchTask(b *testing.B) (*models.SyncTask, *HashCache) {
	b.Helper()
	src := b.TempDir()
	dst := b.TempDir()
	buildBenchTree(b, src)
	task := models.NewSyncTask("bench", src, dst, nil)
	cache := NewHashCache(filepath.Join(b.TempDir(), "cache.gob"))
	if _, err := Run(context.Background(), task, cache, NewTracker("bench"), Options{}); err != nil {
		b.Fatal(err)
	}
	return task, cache
}

func BenchmarkSteadyStatePipeline(b *testing.B) {
	task, _ := benchTask(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cache := NewHashCache(filepath.Join(b.TempDir(), "cache.gob"))
		if _, err := Run(context.Background(), task, cache, NewTracker("bench"), Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOldFullHashBothSides(b *testing.B) {
	task, _ := benchTask(b)
	src, dst := task.SourcePath, task.TargetPath
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := 0
		for _, root := range []string{src, dst} {
			walkFiles(root, func(abs string) {
				if _, err := computeHash(abs); err != nil {
					b.Fatal(err)
				}
				n++
			})
		}
		if n != 2*benchDirs*benchFilesPer {
			b.Fatalf("hashed %d files", n)
		}
	}
}
