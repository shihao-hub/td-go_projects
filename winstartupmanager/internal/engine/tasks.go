package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"winstartupmanager/internal/model"
)

// 触发器类型白名单（TASK_TRIGGER_TYPE2 枚举）：自启动与定时周期触发。
const (
	TASK_TRIGGER_TIME       = 1 // 一次/间隔重复（如每小时巡检）
	TASK_TRIGGER_DAILY      = 2
	TASK_TRIGGER_WEEKLY     = 3
	TASK_TRIGGER_MONTHLY    = 4
	TASK_TRIGGER_MONTHLYDOW = 5
	TASK_TRIGGER_BOOT       = 8
	TASK_TRIGGER_LOGON      = 9
)

// earliestValidTaskDate 过滤 COM DATE 零值（1899-12-30 等，表示「从未运行/未排期」）。
var earliestValidTaskDate = time.Date(1900, 1, 1, 0, 0, 0, 0, time.Local)

// collectibleTrigger 判定触发器类型是否纳入采集（自启动与定时周期）。
func collectibleTrigger(t int) bool {
	switch t {
	case TASK_TRIGGER_TIME, TASK_TRIGGER_DAILY, TASK_TRIGGER_WEEKLY,
		TASK_TRIGGER_MONTHLY, TASK_TRIGGER_MONTHLYDOW, TASK_TRIGGER_BOOT, TASK_TRIGGER_LOGON:
		return true
	}
	return false
}

// durationToken 匹配 ISO8601 duration 的数字+单位 token（PT1H / PT30M / P1D）。
var durationToken = regexp.MustCompile(`(\d+)([HMD])`)

// ListScheduledTasks scans Windows Task Scheduler for auto-start and periodic tasks.
func ListScheduledTasks() ([]model.StartupItem, error) {
	_ = ole.CoInitialize(0)
	defer ole.CoUninitialize()

	service, err := createDispatch("Schedule.Service")
	if err != nil {
		return nil, err
	}
	defer service.Release()

	_, err = oleutil.CallMethod(service, "Connect")
	if err != nil {
		return nil, err
	}

	rootRes, err := oleutil.CallMethod(service, "GetFolder", `\`)
	if err != nil {
		return nil, err
	}
	rootFolder := rootRes.ToIDispatch()
	defer rootFolder.Release()

	var items []model.StartupItem
	collectFolderTasks(rootFolder, &items, 0)
	return items, nil
}

func collectFolderTasks(folder *ole.IDispatch, items *[]model.StartupItem, depth int) {
	if depth > 2 {
		return
	}

	// 1. Enumerate tasks in current folder
	tasksRes, err := oleutil.CallMethod(folder, "GetTasks", 1) // 1 = include hidden
	if err == nil && tasksRes.Value() != nil {
		tasks := tasksRes.ToIDispatch()
		countProp, _ := oleutil.GetProperty(tasks, "Count")
		count := int(countProp.Val)

		for i := 1; i <= count; i++ {
			taskRes, err := oleutil.GetProperty(tasks, "Item", i)
			if err != nil || taskRes.Value() == nil {
				continue
			}
			task := taskRes.ToIDispatch()
			item, ok := parseScheduledTask(task)
			task.Release()
			if ok {
				*items = append(*items, item)
			}
		}
		tasks.Release()
	}

	// 2. Enumerate subfolders
	subfoldersRes, err := oleutil.CallMethod(folder, "GetFolders", 0)
	if err == nil && subfoldersRes.Value() != nil {
		subfolders := subfoldersRes.ToIDispatch()
		countProp, _ := oleutil.GetProperty(subfolders, "Count")
		count := int(countProp.Val)

		for i := 1; i <= count; i++ {
			subRes, err := oleutil.GetProperty(subfolders, "Item", i)
			if err != nil || subRes.Value() == nil {
				continue
			}
			subFolder := subRes.ToIDispatch()
			nameProp, _ := oleutil.GetProperty(subFolder, "Name")
			subName := ""
			if nameProp.Value() != nil {
				subName = nameProp.ToString()
			}
			// Skip deep Microsoft internal OS maintenance tasks to stay lightweight & fast
			if !strings.EqualFold(subName, "Microsoft") {
				collectFolderTasks(subFolder, items, depth+1)
			}
			subFolder.Release()
		}
		subfolders.Release()
	}
}

func parseScheduledTask(task *ole.IDispatch) (model.StartupItem, bool) {
	name := dispatchString(task, "Name")
	taskPath := dispatchString(task, "Path")
	if taskPath == "" {
		taskPath = name
	}

	enabled := true
	if enabledRes, err := oleutil.GetProperty(task, "Enabled"); err == nil {
		if b, ok := enabledRes.Value().(bool); ok {
			enabled = b
		}
	}

	defRes, err := oleutil.GetProperty(task, "Definition")
	if err != nil || defRes.Value() == nil {
		return model.StartupItem{}, false
	}
	definition := defRes.ToIDispatch()
	defer definition.Release()

	// Triggers: 采集须命中白名单；同时生成人读描述
	triggerDesc, hasTrigger := scanTriggers(definition)
	if !hasTrigger {
		return model.StartupItem{}, false
	}

	// Extract Action（COM 属性读取全走带 err 防护的 helper，失败按空串处理）
	exePath := ""
	args := ""
	actionsRes, err := oleutil.GetProperty(definition, "Actions")
	if err == nil && actionsRes.Value() != nil {
		actions := actionsRes.ToIDispatch()
		if actCount, ok := dispatchInt(actions, "Count"); ok && actCount > 0 {
			actRes, err := oleutil.GetProperty(actions, "Item", 1)
			if err == nil && actRes.Value() != nil {
				act := actRes.ToIDispatch()
				exePath = dispatchString(act, "Path")
				args = dispatchString(act, "Arguments")
				act.Release()
			}
		}
		actions.Release()
	}

	cleanExe := cleanExePath(exePath)
	resolved, fileExists := resolveExePath(cleanExe)

	publisher := ""
	if fileExists {
		publisher = GetFilePublisher(resolved)
	} else if cleanExe != "" {
		publisher = "文件不存在"
	} else {
		publisher = "无执行体"
	}

	rawCommand := cleanExe
	if args != "" {
		rawCommand += " " + args
	}

	return model.StartupItem{
		ID:          model.GenerateID(model.SourceTaskScheduler, taskPath),
		Source:      model.SourceTaskScheduler,
		Name:        name,
		RawCommand:  rawCommand,
		ExePath:     cleanExe,
		Args:        args,
		IsEnabled:   enabled,
		FileExists:  fileExists,
		Publisher:   publisher,
		IsElevated:  true,
		TriggerDesc: triggerDesc,
		TaskPath:    taskPath,
		NextRun:     taskTimeText(task, "NextRunTime"),
		LastRun:     taskTimeText(task, "LastRunTime"),
		LastResult:  taskResult(task),
	}, true
}

// scanTriggers 遍历触发器：任一命中白名单即采集，并生成 "、" 连接的人读描述。
func scanTriggers(definition *ole.IDispatch) (string, bool) {
	triggersRes, err := oleutil.GetProperty(definition, "Triggers")
	if err != nil || triggersRes.Value() == nil {
		return "", false
	}
	triggers := triggersRes.ToIDispatch()
	defer triggers.Release()

	countProp, _ := oleutil.GetProperty(triggers, "Count")
	count := int(countProp.Val)

	var parts []string
	for j := 1; j <= count; j++ {
		trigRes, err := oleutil.GetProperty(triggers, "Item", j)
		if err != nil || trigRes.Value() == nil {
			continue
		}
		trig := trigRes.ToIDispatch()

		trigType, typeOk := dispatchInt(trig, "Type")
		if !typeOk || !collectibleTrigger(trigType) {
			trig.Release()
			continue
		}
		parts = append(parts, describeTrigger(trig, trigType))
		trig.Release()
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "、"), true
}

// describeTrigger 生成单个触发器的人读描述。
func describeTrigger(trig *ole.IDispatch, trigType int) string {
	switch trigType {
	case TASK_TRIGGER_BOOT:
		return "开机时"
	case TASK_TRIGGER_LOGON:
		return "登录时"
	case TASK_TRIGGER_TIME:
		repetitionDesc := ""
		if repRes, err := oleutil.GetProperty(trig, "Repetition"); err == nil && repRes.Value() != nil {
			repetitionDesc = intervalText(dispatchString(repRes.ToIDispatch(), "Interval"))
		}
		if repetitionDesc != "" {
			return repetitionDesc
		}
		return "定时触发"
	case TASK_TRIGGER_DAILY:
		if days, ok := dispatchInt(trig, "DaysInterval"); ok && days > 1 {
			return fmt.Sprintf("每 %d 天", days)
		}
		return "每天"
	case TASK_TRIGGER_WEEKLY:
		if weeks, ok := dispatchInt(trig, "WeeksInterval"); ok && weeks > 1 {
			return fmt.Sprintf("每 %d 周", weeks)
		}
		return "每周"
	case TASK_TRIGGER_MONTHLY, TASK_TRIGGER_MONTHLYDOW:
		return "每月"
	}
	return fmt.Sprintf("类型 %d", trigType)
}

// intervalText 把 ISO8601 duration（PT1H / PT30M / P1D）转为「每小时」式描述。
func intervalText(interval string) string {
	if interval == "" {
		return ""
	}
	minutes := 0
	for _, m := range durationToken.FindAllStringSubmatch(strings.ToUpper(interval), -1) {
		n := 0
		fmt.Sscanf(m[1], "%d", &n)
		switch m[2] {
		case "H":
			minutes += n * 60
		case "M":
			minutes += n
		case "D":
			minutes += n * 24 * 60
		}
	}
	if minutes <= 0 {
		return ""
	}
	switch {
	case minutes == 1440:
		return "每天"
	case minutes == 60:
		return "每小时"
	case minutes%1440 == 0:
		return fmt.Sprintf("每 %d 天", minutes/1440)
	case minutes%60 == 0:
		return fmt.Sprintf("每 %d 小时", minutes/60)
	default:
		return fmt.Sprintf("每 %d 分钟", minutes)
	}
}

// dispatchString 读取 COM 属性字符串，失败返回空串。
func dispatchString(disp *ole.IDispatch, prop string) string {
	res, err := oleutil.GetProperty(disp, prop)
	if err != nil || res.Value() == nil {
		return ""
	}
	return res.ToString()
}

// dispatchInt 读取 COM 属性整数值。
func dispatchInt(disp *ole.IDispatch, prop string) (int, bool) {
	res, err := oleutil.GetProperty(disp, prop)
	if err != nil || res.Value() == nil {
		return 0, false
	}
	if v, ok := res.Value().(int32); ok {
		return int(v), true
	}
	return 0, false
}

// taskTimeText 把 COM DATE 属性转本地时间文本；无排期/从未运行返回空串。
func taskTimeText(task *ole.IDispatch, prop string) string {
	res, err := oleutil.GetProperty(task, prop)
	if err != nil || res.Value() == nil {
		return ""
	}
	var t time.Time
	switch v := res.Value().(type) {
	case time.Time:
		t = v
	case float64: // COM DATE 底层是双精度天数，自 1899-12-30 起
		t = oleDateToTime(v)
	default:
		return ""
	}
	if t.Before(earliestValidTaskDate) {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// oleDateToTime 把 COM DATE 序列值（双精度天数）转换为时间。
func oleDateToTime(days float64) time.Time {
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(days * 24 * float64(time.Hour)))
}

// taskResult 读取上次运行退出码（0 = 正常）。
func taskResult(task *ole.IDispatch) int32 {
	res, err := oleutil.GetProperty(task, "LastTaskResult")
	if err != nil || res.Value() == nil {
		return 0
	}
	switch v := res.Value().(type) {
	case int32:
		return v
	case int64:
		return int32(v)
	}
	return 0
}
