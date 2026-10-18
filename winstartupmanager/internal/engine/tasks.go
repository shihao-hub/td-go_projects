package engine

import (
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"winstartupmanager/internal/model"
)

const (
	TASK_TRIGGER_BOOT  = 8
	TASK_TRIGGER_LOGON = 9
)

// ListScheduledTasks scans Windows Task Scheduler for logon or boot triggered tasks.
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
	nameProp, _ := oleutil.GetProperty(task, "Name")
	name := ""
	if nameProp.Value() != nil {
		name = nameProp.ToString()
	}

	enabledProp, _ := oleutil.GetProperty(task, "Enabled")
	enabled := true
	if enabledProp.Value() != nil {
		if b, ok := enabledProp.Value().(bool); ok {
			enabled = b
		}
	}

	defRes, err := oleutil.GetProperty(task, "Definition")
	if err != nil || defRes.Value() == nil {
		return model.StartupItem{}, false
	}
	definition := defRes.ToIDispatch()
	defer definition.Release()

	// Check triggers
	hasLogonOrBoot := false
	triggersRes, err := oleutil.GetProperty(definition, "Triggers")
	if err == nil && triggersRes.Value() != nil {
		triggers := triggersRes.ToIDispatch()
		trigCountProp, _ := oleutil.GetProperty(triggers, "Count")
		trigCount := int(trigCountProp.Val)
		for j := 1; j <= trigCount; j++ {
			trigRes, err := oleutil.GetProperty(triggers, "Item", j)
			if err != nil || trigRes.Value() == nil {
				continue
			}
			trig := trigRes.ToIDispatch()
			typeProp, _ := oleutil.GetProperty(trig, "Type")
			trigType := int(typeProp.Val)
			trig.Release()
			if trigType == TASK_TRIGGER_BOOT || trigType == TASK_TRIGGER_LOGON {
				hasLogonOrBoot = true
				break
			}
		}
		triggers.Release()
	}

	if !hasLogonOrBoot {
		return model.StartupItem{}, false
	}

	// Extract Action
	exePath := ""
	args := ""
	actionsRes, err := oleutil.GetProperty(definition, "Actions")
	if err == nil && actionsRes.Value() != nil {
		actions := actionsRes.ToIDispatch()
		actCountProp, _ := oleutil.GetProperty(actions, "Count")
		if int(actCountProp.Val) > 0 {
			actRes, err := oleutil.GetProperty(actions, "Item", 1)
			if err == nil && actRes.Value() != nil {
				act := actRes.ToIDispatch()
				pathProp, _ := oleutil.GetProperty(act, "Path")
				if pathProp.Value() != nil {
					exePath = pathProp.ToString()
				}
				argsProp, _ := oleutil.GetProperty(act, "Arguments")
				if argsProp.Value() != nil {
					args = argsProp.ToString()
				}
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
		ID:         model.GenerateID(model.SourceTaskScheduler, name),
		Source:     model.SourceTaskScheduler,
		Name:       name,
		RawCommand: rawCommand,
		ExePath:    cleanExe,
		Args:       args,
		IsEnabled:  enabled,
		FileExists: fileExists,
		Publisher:  publisher,
		IsElevated: true,
	}, true
}