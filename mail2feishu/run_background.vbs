' mail2feishu 后台静默启动脚本。
' 优先启动正式版 bin\mail2feishu.exe，不存在时回退开发版 bin\mail2feishu-dev.exe。
' 隐藏窗口运行；日志写入数据目录（%APPDATA%\language_projects\mail2feishu\<dev|prod>\mail2feishu.log）。
Option Explicit

Dim fso, shell, scriptDir, exePath
Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")

scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)
exePath = fso.BuildPath(scriptDir, "bin\mail2feishu.exe")
If Not fso.FileExists(exePath) Then
    exePath = fso.BuildPath(scriptDir, "bin\mail2feishu-dev.exe")
End If

If Not fso.FileExists(exePath) Then
    MsgBox "未找到 mail2feishu.exe 或 mail2feishu-dev.exe，请先运行 uv run scripts/build.py", vbExclamation, "mail2feishu"
    WScript.Quit 1
End If

shell.CurrentDirectory = scriptDir
' 0 = 隐藏窗口，False = 不等待进程结束
shell.Run """" & exePath & """", 0, False
