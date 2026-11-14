' mail2feishu 后台静默启动：拉起 GUI 通道的隐藏托盘模式。
' 优先正式版 bin\mail2feishu-gui.exe，不存在时回退开发版 bin\mail2feishu-gui-dev.exe。
' 开机自启请使用托盘菜单（仅 release 版可注册），本脚本只是手动静默入口。
Option Explicit

Dim fso, shell, scriptDir, exePath
Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")

scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)
exePath = fso.BuildPath(scriptDir, "bin\mail2feishu-gui.exe")
If Not fso.FileExists(exePath) Then
    exePath = fso.BuildPath(scriptDir, "bin\mail2feishu-gui-dev.exe")
End If

If Not fso.FileExists(exePath) Then
    MsgBox "未找到 mail2feishu-gui.exe 或 mail2feishu-gui-dev.exe，请先运行 uv run scripts/build.py", vbExclamation, "mail2feishu"
    WScript.Quit 1
End If

shell.CurrentDirectory = scriptDir
' 0 = 隐藏窗口，False = 不等待进程结束
shell.Run """" & exePath & """ --hidden", 0, False
