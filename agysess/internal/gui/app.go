package gui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"agysess/internal/cli"
	"agysess/internal/model"
	"agysess/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// EnsureChineseFont 注入系统默认中文字体（必须为 .ttf 单字体，Fyne 不支持 .ttc 集合字体）
func EnsureChineseFont() {
	if os.Getenv("FYNE_FONT") == "" {
		candidates := []string{
			`C:\Windows\Fonts\simhei.ttf`,
			`C:\Windows\Fonts\YaHei Consolas Hybrid 1.12.ttf`,
			`C:\Windows\Fonts\msyh.ttf`,
			`C:\Windows\Fonts\simkai.ttf`,
			`C:\Windows\Fonts\simfang.ttf`,
		}
		for _, fontPath := range candidates {
			if _, err := os.Stat(fontPath); err == nil {
				_ = os.Setenv("FYNE_FONT", fontPath)
				break
			}
		}
	}
}

// AppState GUI 响应式运行时状态
type AppState struct {
	svc          *service.SessionService
	window       fyne.Window
	sessions     []model.Session
	stats        model.SessionStats
	searchQuery  string
	selectedProj string
	viewMode     string // "active", "all", "archived"
	sortBy       string
	projects     []string
}

// RunGUI 启动 Fyne 原生轻量桌面面板
func RunGUI(svc *service.SessionService) error {
	EnsureChineseFont()

	a := app.NewWithID("com.antigravity.agysess")
	w := a.NewWindow("agysess - Antigravity CLI 会话管理器")
	w.Resize(fyne.NewSize(980, 680))

	state := &AppState{
		svc:          svc,
		window:       w,
		viewMode:     "active",
		sortBy:       "time_desc",
		selectedProj: "全部项目",
	}

	// 初始加载项目列表与会话列表
	ctx := context.Background()
	if projs, err := svc.ListProjects(ctx); err == nil {
		state.projects = append([]string{"全部项目"}, projs...)
	}

	// 控件声明
	statusBar := widget.NewLabel("正在加载会话数据...")

	var sessionList *widget.List
	refreshData := func() {
		filterOpts := model.FilterOptions{
			Query:  state.searchQuery,
			SortBy: state.sortBy,
		}
		if state.selectedProj != "全部项目" {
			filterOpts.ProjectID = state.selectedProj
		}

		switch state.viewMode {
		case "archived":
			filterOpts.ArchivedOnly = true
		case "all":
			filterOpts.IncludeArchived = true
		default: // "active"
			filterOpts.IncludeArchived = false
		}

		sess, stats, err := svc.ListSessions(context.Background(), filterOpts)
		if err != nil {
			dialog.ShowError(fmt.Errorf("加载会话失败: %w", err), w)
			return
		}

		state.sessions = sess
		state.stats = stats
		statusBar.SetText(fmt.Sprintf("显示 %d 条会话 (总计: %d | 活跃: %d | 已归档: %d | 运行中: %d)  |  数据源: %s",
			len(sess), stats.Total, stats.Active, stats.Archived, stats.Running, svc.DBPath()))

		if sessionList != nil {
			sessionList.Refresh()
		}
	}

	// 顶部工具栏
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("搜索标题、预览文本、ID...")
	searchEntry.OnChanged = func(s string) {
		state.searchQuery = s
		refreshData()
	}

	projSelect := widget.NewSelect(state.projects, func(s string) {
		state.selectedProj = s
		refreshData()
	})
	projSelect.SetSelected("全部项目")

	viewSelect := widget.NewSelect([]string{"活跃会话", "全部会话", "已归档"}, func(s string) {
		switch s {
		case "已归档":
			state.viewMode = "archived"
		case "全部会话":
			state.viewMode = "all"
		default:
			state.viewMode = "active"
		}
		refreshData()
	})
	viewSelect.SetSelected("活跃会话")

	sortSelect := widget.NewSelect([]string{"按更新时间", "按步数倒序", "按标题字母"}, func(s string) {
		switch s {
		case "按步数倒序":
			state.sortBy = "steps_desc"
		case "按标题字母":
			state.sortBy = "title_asc"
		default:
			state.sortBy = "time_desc"
		}
		refreshData()
	})
	sortSelect.SetSelected("按更新时间")

	refreshBtn := widget.NewButtonWithIcon("刷新", theme.ViewRefreshIcon(), func() {
		refreshData()
	})

	newBtn := widget.NewButtonWithIcon("新建会话", theme.ContentAddIcon(), func() {
		showNewSessionDialog(w, svc, refreshData)
	})
	newBtn.Importance = widget.HighImportance

	topControls := container.NewBorder(
		nil, nil,
		nil,
		container.NewHBox(projSelect, viewSelect, sortSelect, refreshBtn, newBtn),
		searchEntry,
	)

	// 中间会话列表
	sessionList = widget.NewList(
		func() int {
			return len(state.sessions)
		},
		func() fyne.CanvasObject {
			titleLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			badgeLabel := widget.NewLabel("")
			metaLabel := widget.NewLabel("")
			previewLabel := widget.NewLabel("")

			openBtn := widget.NewButtonWithIcon("在 WT 打开", theme.MediaPlayIcon(), nil)
			openBtn.Importance = widget.HighImportance

			killBtn := widget.NewButtonWithIcon("关闭窗口", theme.CancelIcon(), nil)
			killBtn.Importance = widget.DangerImportance

			archiveBtn := widget.NewButtonWithIcon("归档", theme.FileTextIcon(), nil)
			editBtn := widget.NewButtonWithIcon("编辑", theme.DocumentCreateIcon(), nil)

			btnBox := container.NewHBox(openBtn, killBtn, archiveBtn, editBtn)

			headerRow := container.NewBorder(nil, nil, badgeLabel, btnBox, titleLabel)
			contentBox := container.NewVBox(headerRow, metaLabel, previewLabel)

			return container.NewPadded(contentBox)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < 0 || id >= len(state.sessions) {
				return
			}
			sess := state.sessions[id]

			pad := item.(*fyne.Container)
			contentBox := pad.Objects[0].(*fyne.Container)
			headerRow := contentBox.Objects[0].(*fyne.Container)
			badgeLabel := headerRow.Objects[0].(*widget.Label)
			btnBox := headerRow.Objects[1].(*fyne.Container)
			titleLabel := headerRow.Objects[2].(*widget.Label)

			metaLabel := contentBox.Objects[1].(*widget.Label)
			previewLabel := contentBox.Objects[2].(*widget.Label)

			openBtn := btnBox.Objects[0].(*widget.Button)
			killBtn := btnBox.Objects[1].(*widget.Button)
			archiveBtn := btnBox.Objects[2].(*widget.Button)
			editBtn := btnBox.Objects[3].(*widget.Button)

			// 填充数据
			displayTitle := sess.DisplayTitle()
			titleLabel.SetText(cli.TruncateString(displayTitle, 40))

			if sess.IsRunning {
				badgeLabel.SetText(fmt.Sprintf("● [RUNNING: %d]", sess.PID))
				killBtn.Show()
			} else {
				if sess.IsArchived {
					badgeLabel.SetText("■ [已归档]")
				} else {
					badgeLabel.SetText("○ [活跃]")
				}
				killBtn.Hide()
			}

			projText := sess.ProjectID
			if projText == "" {
				projText = "未分类"
			}
			timeStr := cli.FormatTimeRelative(sess.LastModifiedTime)
			metaLabel.SetText(fmt.Sprintf("项目: %s  |  步数: %d 步  |  更新: %s  |  工作区: %s",
				projText, sess.StepCount, timeStr, sess.WorkspaceURIs))

			previewText := sess.Preview
			if sess.CustomNotes != "" {
				previewText = fmt.Sprintf("【备注: %s】 %s", sess.CustomNotes, previewText)
			}
			previewLabel.SetText(cli.TruncateString(previewText, 80))

			if sess.IsArchived {
				archiveBtn.SetText("解档")
				archiveBtn.SetIcon(theme.ContentUndoIcon())
			} else {
				archiveBtn.SetText("归档")
				archiveBtn.SetIcon(theme.FileTextIcon())
			}

			// 绑定事件
			currID := sess.ID
			openBtn.OnTapped = func() {
				if err := svc.OpenSession(context.Background(), currID, false); err != nil {
					dialog.ShowError(err, w)
				} else {
					// 稍等 1 秒刷新运行态
					go func() {
						time.Sleep(1 * time.Second)
						fyne.Do(refreshData)
					}()
				}
			}

			killBtn.OnTapped = func() {
				dialog.ShowConfirm("确认关闭窗口",
					fmt.Sprintf("确定要终止会话 [%s] (PID: %d) 并回收终端窗口吗？", displayTitle, sess.PID),
					func(ok bool) {
						if ok {
							if _, err := svc.KillSession(context.Background(), currID); err != nil {
								dialog.ShowError(err, w)
							} else {
								refreshData()
							}
						}
					}, w)
			}

			archiveBtn.OnTapped = func() {
				var err error
				if sess.IsArchived {
					err = svc.UnarchiveSession(context.Background(), currID)
				} else {
					err = svc.ArchiveSession(context.Background(), currID)
				}
				if err != nil {
					dialog.ShowError(err, w)
				} else {
					refreshData()
				}
			}

			editBtn.OnTapped = func() {
				showEditDialog(w, svc, sess, refreshData)
			}
		},
	)

	// 初次加载数据
	refreshData()

	mainContent := container.NewBorder(
		container.NewPadded(topControls),
		container.NewPadded(statusBar),
		nil, nil,
		sessionList,
	)

	w.SetContent(mainContent)
	w.ShowAndRun()
	return nil
}

// showNewSessionDialog 新建会话弹窗
func showNewSessionDialog(parent fyne.Window, svc *service.SessionService, onDone func()) {
	cwd, _ := os.Getwd()
	dirEntry := widget.NewEntry()
	dirEntry.SetText(cwd)

	projEntry := widget.NewEntry()
	projEntry.SetPlaceHolder("可选，指定项目名称")

	modelSelect := widget.NewSelect([]string{"", "gemini-2.5-pro", "gemini-2.5-flash"}, nil)
	modeSelect := widget.NewSelect([]string{"", "accept-edits", "auto", "plan"}, nil)
	effortSelect := widget.NewSelect([]string{"", "low", "medium", "high", "xhigh", "max"}, nil)
	promptEntry := widget.NewMultiLineEntry()
	promptEntry.SetPlaceHolder("可选初始 prompt...")

	form := widget.NewForm(
		widget.NewFormItem("工作区路径", dirEntry),
		widget.NewFormItem("项目名称", projEntry),
		widget.NewFormItem("AI 模型", modelSelect),
		widget.NewFormItem("执行模式", modeSelect),
		widget.NewFormItem("思考强度", effortSelect),
		widget.NewFormItem("初始提示词", promptEntry),
	)

	d := dialog.NewCustomConfirm("新建 agy 会话", "在 WT 打开", "取消", form, func(ok bool) {
		if !ok {
			return
		}
		opts := model.CreateSessionOptions{
			WorkspaceDir:  strings.TrimSpace(dirEntry.Text),
			ProjectID:     strings.TrimSpace(projEntry.Text),
			Model:         modelSelect.Selected,
			Mode:          modeSelect.Selected,
			Effort:        effortSelect.Selected,
			InitialPrompt: strings.TrimSpace(promptEntry.Text),
		}
		if err := svc.CreateSession(context.Background(), opts); err != nil {
			dialog.ShowError(err, parent)
		} else {
			if onDone != nil {
				time.Sleep(1 * time.Second)
				onDone()
			}
		}
	}, parent)

	d.Resize(fyne.NewSize(550, 420))
	d.Show()
}

// showEditDialog 编辑会话备注与自定义标题
func showEditDialog(parent fyne.Window, svc *service.SessionService, sess model.Session, onDone func()) {
	titleEntry := widget.NewEntry()
	titleEntry.SetText(sess.CustomTitle)
	if titleEntry.Text == "" {
		titleEntry.SetPlaceHolder(sess.Title)
	}

	notesEntry := widget.NewMultiLineEntry()
	notesEntry.SetText(sess.CustomNotes)
	notesEntry.SetPlaceHolder("输入自定义备注信息...")

	form := widget.NewForm(
		widget.NewFormItem("自定义标题", titleEntry),
		widget.NewFormItem("会话备注", notesEntry),
	)

	d := dialog.NewCustomConfirm("编辑会话元数据", "保存", "取消", form, func(ok bool) {
		if !ok {
			return
		}
		ctx := context.Background()
		newTitle := strings.TrimSpace(titleEntry.Text)
		newNotes := strings.TrimSpace(notesEntry.Text)

		if err := svc.UpdateCustomTitle(ctx, sess.ID, newTitle); err != nil {
			dialog.ShowError(err, parent)
			return
		}
		if err := svc.UpdateNotes(ctx, sess.ID, newNotes); err != nil {
			dialog.ShowError(err, parent)
			return
		}

		if onDone != nil {
			onDone()
		}
	}, parent)

	d.Resize(fyne.NewSize(500, 300))
	d.Show()
}
