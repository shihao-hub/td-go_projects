package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// psCompletionScript PowerShell 补全脚本（PS 5.1+ 兼容）。
// 约束：脚本内不得使用反引号（Go raw string 语法定界符）。
// 行为：只绑定 clictl 命令名；第 1 位置补全子命令名；run/start/stop/rm/info/set
// 后的第 1 位置补全工具名；run/start 第 2 参数起为子进程透传段，不产生候选；
// 跳过 --pretty 等 flag 计位；内部 try/catch 静默失败，绝不打扰 Tab 体验。
const psCompletionScript = `# clictl PowerShell 补全（由 'clictl completion powershell' 生成，请勿手改）
Register-ArgumentCompleter -Native -CommandName clictl -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    try {
        $elems = @($commandAst.CommandElements | Select-Object -Skip 1 | Where-Object { "$_" -notlike '-*' })
        # 光标处正在输入的 wordToComplete 已是 CommandElements 的一员，计数前须排除，
        # 否则 'clictl run ze<Tab>' 里 ze 会被当成已确认的位置参数
        if ($elems.Count -gt 0 -and $wordToComplete -ne '' -and "$($elems[-1])" -eq $wordToComplete) {
            $elems = @($elems | Select-Object -First ($elems.Count - 1))
        }
        if ($elems.Count -eq 0) {
            'add','rm','set','list','info','run','start','stop','version','help','completion' |
                Where-Object { $_ -like "$wordToComplete*" }
            return
        }
        $sub = "$($elems[0])"
        if (@('run','start','stop','rm','info','set') -contains $sub -and $elems.Count -eq 1) {
            & clictl completion names 2>$null | Where-Object { $_ -like "$wordToComplete*" }
        }
    } catch { }
}`

// $PROFILE 安装块标记（conda-init 风格，成对出现，卸载按块识别）
const (
	installBegin = "# >>> clictl completion >>>"
	installEnd   = "# <<< clictl completion <<<"
	// 守卫：clictl 不在 PATH 的会话（如定向环境）也不污染 shell 启动
	psInstallLine = "if (Get-Command clictl -ErrorAction SilentlyContinue) { clictl completion powershell | Out-String | Invoke-Expression }"
)

// cmdCompletion 补全子命令：
//
//	completion powershell [--install|--uninstall]  输出/安装/卸载 PowerShell 补全脚本
//	completion names                               输出全部工具名（每行一个）
//
// 输出约定：powershell（脚本）与 names 的消费者是 shell，输出 raw 文本，
// 是"管理命令永远 JSON"约定的例外（同 run stdout 只属于子进程的例外先例）；
// --install/--uninstall 为动作型命令，仍输出 JSON 包络
func cmdCompletion(args []string) int {
	if len(args) == 0 {
		Fail("bad_args", "用法: clictl completion <powershell|names> [--install|--uninstall]")
		return 1
	}
	target := args[0]
	if target != "powershell" && target != "names" {
		Fail("bad_args", "仅支持: powershell | names")
		return 1
	}
	fs := newFlagSet("completion")
	install := fs.Bool("install", false, "把补全脚本写入 PowerShell $PROFILE")
	uninstall := fs.Bool("uninstall", false, "从 PowerShell $PROFILE 移除补全脚本")
	if !parseFlags(fs, args[1:]) {
		return 0
	}
	if fs.NArg() > 0 {
		Fail("bad_args", "completion 不接受额外位置参数: "+strings.Join(fs.Args(), " "))
		return 1
	}
	if *install && *uninstall {
		Fail("bad_args", "--install 与 --uninstall 互斥")
		return 1
	}
	if target == "names" && (*install || *uninstall) {
		Fail("bad_args", "names 无需安装")
		return 1
	}

	if target == "names" {
		tools, err := mustStore().ListTools("")
		if err != nil {
			failFromErr("completion", err)
			return 1
		}
		for _, t := range tools {
			fmt.Println(t.Name)
		}
		return 0
	}

	switch {
	case *uninstall:
		return psUninstall()
	case *install:
		return psInstall()
	default:
		fmt.Println(psCompletionScript)
		return 0
	}
}

// psProfilePath 定位 PowerShell $PROFILE 真实路径。
// 优先让 PowerShell 自己回答（正确处理 Documents/OneDrive 重定向，
// 输出强制 UTF-8 防中文用户名乱码）；失败回退默认路径拼接
func psProfilePath() (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $PROFILE")
	if out, err := cmd.Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return p, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %w", err)
	}
	return filepath.Join(home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"), nil
}

// psInstall 把补全安装块写入 $PROFILE（幂等：已有标记则跳过）
func psInstall() int {
	profile, err := psProfilePath()
	if err != nil {
		Fail("bad_args", "定位 $PROFILE 失败: "+err.Error())
		return 1
	}
	content := ""
	if b, err := os.ReadFile(profile); err == nil {
		content = string(b)
	}
	if strings.Contains(content, installBegin) {
		Emit(map[string]any{"installed": true, "already": true, "profile": profile})
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
		Fail("internal", "创建 $PROFILE 目录失败: "+err.Error())
		return 1
	}
	var sb strings.Builder
	sb.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		sb.WriteString("\r\n")
	}
	sb.WriteString(installBegin + "\r\n")
	sb.WriteString(psInstallLine + "\r\n")
	sb.WriteString(installEnd + "\r\n")
	if err := os.WriteFile(profile, []byte(sb.String()), 0o644); err != nil {
		Fail("internal", "写入 $PROFILE 失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"installed": true, "already": false, "profile": profile})
	return 0
}

// psUninstall 从 $PROFILE 移除补全安装块（按标记整块删除，幂等）
func psUninstall() int {
	profile, err := psProfilePath()
	if err != nil {
		Fail("bad_args", "定位 $PROFILE 失败: "+err.Error())
		return 1
	}
	b, err := os.ReadFile(profile)
	if err != nil {
		Emit(map[string]any{"removed": 0, "profile": profile})
		return 0
	}
	content := string(b)
	hasBegin := strings.Contains(content, installBegin)
	hasEnd := strings.Contains(content, installEnd)
	if !hasBegin && !hasEnd {
		Emit(map[string]any{"removed": 0, "profile": profile})
		return 0
	}
	// 只有一半标记（用户手改过 profile）时拒绝整块删除，避免误删其他内容
	if !hasBegin || !hasEnd {
		Fail("bad_profile", "$PROFILE 中补全标记不完整（缺 begin/end），请手动删除相关行: "+profile)
		return 1
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := 0
	inBlock := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == installBegin {
			inBlock = true
			removed++
			continue
		}
		if t == installEnd {
			inBlock = false
			removed++
			continue
		}
		if inBlock {
			removed++
			continue
		}
		out = append(out, line)
	}
	if inBlock {
		Fail("bad_profile", "$PROFILE 中补全标记不完整，请手动删除相关行: "+profile)
		return 1
	}
	if err := os.WriteFile(profile, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		Fail("internal", "写回 $PROFILE 失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"removed": removed, "profile": profile})
	return 0
}
