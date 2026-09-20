// Package notify 封装 Windows Toast 通知（github.com/go-toast/toast）。
// go-toast 的实现方式是生成 PowerShell 脚本调用 WinRT 投影，因此：
//   - 通知文案经由 PS 模板，需兜底移除会破坏模板的字符序列（见 sanitize）；
//   - 未注册 AUMID 在 Win10 1709+/Win11 会被系统静默丢弃且 Push 不返回错误，
//     所以默认用 PowerShell 自身的注册 AUMID（通知归在 "Windows PowerShell" 名下）；
//   - 静默丢弃（勿扰/通知总开关关闭）与硬失败（powershell 不在 PATH）要区分
//     排查，见 README「通知不弹排查」。
//
// 本包不做单元测试（依赖外部 PowerShell），靠真机验证；通知逻辑刻意收敛在
// 单文件内，将来可无痛替换 jackmordaunt/go-toast 或 gen2brain/beeep。
package notify

import (
	"strings"

	"github.com/go-toast/toast"
)

// DefaultAppID PowerShell 自身的注册 AUMID（Win11 必有注册，弹横幅且进操作中心）。
const DefaultAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// Toast 基于 go-toast 的通知器，实现 service.Notifier。
type Toast struct {
	AppID  string // 空值用 DefaultAppID
	Silent bool   // true 时静音
}

// Notify 发送一条 Toast。返回 error 仅代表硬失败（脚本执行失败等），
// 系统侧静默丢弃不产生错误。
func (t Toast) Notify(title, msg string) error {
	appID := t.AppID
	if strings.TrimSpace(appID) == "" {
		appID = DefaultAppID
	}
	n := toast.Notification{
		AppID:    sanitize(appID, "'"),
		Title:    sanitize(title, "]]>"),
		Message:  sanitize(msg, "]]>"),
		Audio:    toast.Default,
		Duration: toast.Short,
	}
	if t.Silent {
		n.Audio = toast.Silent
	}
	return n.Push()
}

// sanitize 兜底移除会破坏 go-toast PowerShell 模板的字符序列：
// AppID 位于单引号字符串中（单引号致命），Title/Message 位于 XML CDATA
// 中（"]>"结束序列致命）。本工具文案均为程序生成，正常不会命中。
func sanitize(s, bad string) string {
	return strings.ReplaceAll(s, bad, " ")
}
