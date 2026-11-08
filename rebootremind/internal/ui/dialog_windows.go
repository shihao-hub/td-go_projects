//go:build windows

// Package ui 提供 Windows 原生 TaskDialog 提醒弹窗。
package ui

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"rebootremind/internal/model"
)

var (
	comctl32                 = windows.NewLazySystemDLL("comctl32.dll")
	procTaskDialogIndirect   = comctl32.NewProc("TaskDialogIndirect")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	initOnce                 sync.Once
)

const (
	// 弹窗按钮 ID。
	buttonSnoozeShort = 1001
	buttonSnoozeLong  = 1002
	buttonRestart     = 1003

	// TaskDialog 标志。
	tdfAllowDialogCancellation = 0x0008
	tdfUseCommandLinks         = 0x0010
	tdfSizeToContent           = 0x01000000

	// 系统图标资源 ID（MAKEINTRESOURCE）。
	tdInformationIcon uintptr = 0xFFFD
	tdWarningIcon     uintptr = 0xFFFF

	// 公共按钮与返回值。
	tdcbfYesButton = 0x0002
	tdcbfNoButton  = 0x0004
	idYes          = 6

	iccWin95Classes = 0x000000FF
)

// commctrl.h 中的 TASKDIALOGCONFIG / TASKDIALOG_BUTTON 被 pshpack1.h 按 1 字节
// 对齐，Go 结构体会自动补齐，因此这里用原始字节缓冲区精确复刻布局。
const (
	tdConfigSize = 160
	tdButtonSize = 12

	offCbSize               = 0
	offHwndParent           = 4
	offHInstance            = 12
	offFlags                = 20
	offCommonButtons        = 24
	offWindowTitle          = 28
	offMainIcon             = 36
	offMainInstruction      = 44
	offContent              = 52
	offButtonCount          = 60
	offButtons              = 64
	offDefaultButton        = 72
	offVerificationText     = 92
	offExpandedInformation  = 100
	offExpandedControlText  = 108
	offCollapsedControlText = 116
	offFooterIcon           = 124
	offFooter               = 132
	offCallback             = 140
	offCallbackData         = 148
	offWidth                = 156
)

type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

type buttonDef struct {
	id   int32
	text string
}

// packedDialog 持有原始配置缓冲区及其引用的 UTF-16 内存，防止被 GC 回收。
type packedDialog struct {
	raw       []byte
	texts     []*uint16
	auxBuffer [][]byte
}

func newPackedDialog() *packedDialog {
	return &packedDialog{raw: make([]byte, tdConfigSize)}
}

func (d *packedDialog) u32(offset int, value uint32) {
	binary.LittleEndian.PutUint32(d.raw[offset:], value)
}

func (d *packedDialog) ptr(offset int, value uintptr) {
	binary.LittleEndian.PutUint64(d.raw[offset:], uint64(value))
}

func (d *packedDialog) str(offset int, text string) {
	if text == "" {
		return
	}
	ptr, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	d.texts = append(d.texts, ptr)
	d.ptr(offset, uintptr(unsafe.Pointer(ptr)))
}

func (d *packedDialog) buttons(offset int, count int, defs []buttonDef) {
	buffer := make([]byte, count*tdButtonSize)
	for index, def := range defs {
		base := index * tdButtonSize
		binary.LittleEndian.PutUint32(buffer[base:], uint32(def.id))
		ptr, err := windows.UTF16PtrFromString(def.text)
		if err != nil {
			continue
		}
		d.texts = append(d.texts, ptr)
		binary.LittleEndian.PutUint64(buffer[base+4:], uint64(uintptr(unsafe.Pointer(ptr))))
	}
	d.auxBuffer = append(d.auxBuffer, buffer)
	d.ptr(offset, uintptr(unsafe.Pointer(&buffer[0])))
}

func (d *packedDialog) call() (int32, error) {
	var pressed int32
	hr, _, _ := procTaskDialogIndirect.Call(
		uintptr(unsafe.Pointer(&d.raw[0])),
		uintptr(unsafe.Pointer(&pressed)),
		0,
		0,
	)
	if int32(hr) < 0 {
		return 0, fmt.Errorf("TaskDialogIndirect 失败: HRESULT 0x%08X", uint32(hr))
	}
	return pressed, nil
}

func initCommonControls() {
	initOnce.Do(func() {
		var icc initCommonControlsEx
		icc.Size = uint32(unsafe.Sizeof(icc))
		icc.ICC = iccWin95Classes
		_, _, _ = procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	})
}

// ShowReminder 弹出重启提醒弹窗并返回用户选择。
func ShowReminder(snapshot model.Snapshot) (Action, error) {
	initCommonControls()

	mainInstruction := fmt.Sprintf(
		"计算机已连续运行 %s，内存占用达 %.0f%%",
		model.FormatUptime(snapshot.UptimeSeconds),
		snapshot.Memory.Percent,
	)
	content := "检测到系统长期未重启，内核缓冲池与软件堆内存存在碎片化累积。" +
		"建议保存当前工作并重启计算机以恢复最佳性能。"

	defs := []buttonDef{
		{id: buttonSnoozeShort, text: "4 小时后再提醒我\n稍后空闲时再处理（推荐）"},
		{id: buttonSnoozeLong, text: "明天再提醒我\n今天不再提示"},
		{id: buttonRestart, text: "立即重启系统\n二次确认后执行安全重启"},
	}

	dialog := newPackedDialog()
	dialog.u32(offCbSize, tdConfigSize)
	dialog.u32(offFlags, tdfAllowDialogCancellation|tdfUseCommandLinks|tdfSizeToContent)
	dialog.str(offWindowTitle, "系统维护提示 - rebootremind")
	dialog.ptr(offMainIcon, tdInformationIcon)
	dialog.str(offMainInstruction, mainInstruction)
	dialog.str(offContent, content)
	dialog.u32(offButtonCount, uint32(len(defs)))
	dialog.buttons(offButtons, len(defs), defs)
	dialog.u32(offDefaultButton, buttonSnoozeShort)
	dialog.str(offExpandedInformation, expandedInfo(snapshot))
	dialog.str(offExpandedControlText, "查看内存占用详情")
	dialog.str(offCollapsedControlText, "隐藏内存占用详情")

	pressed, err := dialog.call()
	if err != nil {
		return ActionDismissed, err
	}
	switch pressed {
	case buttonSnoozeShort:
		return ActionSnoozeShort, nil
	case buttonSnoozeLong:
		return ActionSnoozeLong, nil
	case buttonRestart:
		return ActionRestart, nil
	default:
		return ActionDismissed, nil
	}
}

// ConfirmRestart 弹出二次确认；用户确认返回 true。
func ConfirmRestart() (bool, error) {
	initCommonControls()

	dialog := newPackedDialog()
	dialog.u32(offCbSize, tdConfigSize)
	dialog.u32(offFlags, tdfAllowDialogCancellation|tdfSizeToContent)
	dialog.u32(offCommonButtons, tdcbfYesButton|tdcbfNoButton)
	dialog.str(offWindowTitle, "确认重启 - rebootremind")
	dialog.ptr(offMainIcon, tdWarningIcon)
	dialog.str(offMainInstruction, "确认立即重启计算机？")
	dialog.str(offContent, "未保存的工作可能丢失。重启会清空内核缓冲池与软件堆内存碎片。")
	dialog.u32(offDefaultButton, idYes)

	pressed, err := dialog.call()
	if err != nil {
		return false, err
	}
	return pressed == idYes, nil
}

func expandedInfo(snapshot model.Snapshot) string {
	if len(snapshot.TopProcesses) == 0 {
		return "未采集到进程内存数据。"
	}
	var builder strings.Builder
	builder.WriteString("当前内存大户（按进程名聚合）：\r\n")
	for _, process := range snapshot.TopProcesses {
		fmt.Fprintf(&builder, "· %s：%s（%.1f%%）\r\n", process.Name, formatBytes(process.WorkingSetBytes), process.Percent)
	}
	return builder.String()
}

func formatBytes(bytes uint64) string {
	const (
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	if bytes >= gb {
		return fmt.Sprintf("%.1f GB", float64(bytes)/gb)
	}
	return fmt.Sprintf("%.0f MB", float64(bytes)/mb)
}
