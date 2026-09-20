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
// 行为：只绑定 clictl 命令名；第 1 位置补全子命令名；run/start/stop/rm/info/set/cp
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
            'add','rm','set','list','info','run','start','stop','cp','version','help','completion' |
                Where-Object { $_ -like "$wordToComplete*" }
            return
        }
        $sub = "$($elems[0])"
        if (@('run','start','stop','rm','info','set','cp') -contains $sub -and $elems.Count -eq 1) {
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
	// 编码固化行 1：$OutputEncoding 管 PS 写下游原生进程 stdin 的重编码
	// （PS 5.1 默认 ASCII，原生 exe 间管道中文变 ? 的直接元凶）
	psUtf8OutLine = "$OutputEncoding = [System.Text.Encoding]::UTF8"
	// 编码固化行 2：[Console]::OutputEncoding 管 PS 解码上游原生进程 stdout
	// （中文系统默认 OEM 936/GBK，不设则 UTF-8 输出先被误解码一次）
	psUtf8ConsoleLine = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8"
)

// psBlockLines 标准安装块的全部行（追加与升级重写共用同一来源）。
// 编码行必须位于 installLine 之前：安装行动态拉取补全脚本，Out-String 的
// 解码依赖 [Console]::OutputEncoding 已就位；编码行在 Get-Command 守卫外
// 无条件执行（即使 clictl 不在 PATH，管道编码固化也生效）
func psBlockLines() []string {
	return []string{
		installBegin,
		psUtf8OutLine,
		psUtf8ConsoleLine,
		psInstallLine,
		installEnd,
	}
}

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
		names, err := mustService().Names()
		if err != nil {
			failService(err, false)
			return 1
		}
		for _, n := range names {
			fmt.Println(n)
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

// psInstall 把补全安装块写入 $PROFILE，幂等三态：
//   - 无块 → 追加标准块（installed:true, already:false）
//   - 有块且已含编码固化行 → 跳过（already:true）
//   - 有块但缺编码行（旧版安装）→ 整块重写为标准块（upgraded:true），
//     兑现"旧安装块免重装自动升级"承诺
//
// 标记不完整（缺 begin/end，用户手改过）时不重写，维持现状按已安装处理
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
	if !strings.Contains(content, installBegin) {
		if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
			Fail("internal", "创建 $PROFILE 目录失败: "+err.Error())
			return 1
		}
		var sb strings.Builder
		sb.WriteString(content)
		if content != "" && !strings.HasSuffix(content, "\n") {
			sb.WriteString("\r\n")
		}
		for _, line := range psBlockLines() {
			sb.WriteString(line)
			sb.WriteString("\r\n")
		}
		if err := os.WriteFile(profile, []byte(sb.String()), 0o644); err != nil {
			Fail("internal", "写入 $PROFILE 失败: "+err.Error())
			return 1
		}
		Emit(map[string]any{"installed": true, "already": false, "profile": profile})
		return 0
	}

	// 有 begin 标记：按行定位完整块（TrimSpace 比较，容忍 CRLF）
	lines := strings.Split(content, "\n")
	beginIdx, endIdx := -1, -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == installBegin && beginIdx == -1 {
			beginIdx = i
			continue
		}
		if t == installEnd && beginIdx != -1 {
			endIdx = i
			break
		}
	}
	hasUtf8 := false
	if beginIdx != -1 && endIdx != -1 {
		for _, line := range lines[beginIdx : endIdx+1] {
			if strings.TrimSpace(line) == psUtf8OutLine {
				hasUtf8 = true
				break
			}
		}
	}
	// 块完整且已含编码行，或块不完整（不动用户手改过的 profile）：均按已安装处理
	if hasUtf8 || endIdx == -1 {
		Emit(map[string]any{"installed": true, "already": true, "profile": profile})
		return 0
	}

	// 旧安装块：整块替换为标准块（块内行用 CRLF，其余行原样保留字节）
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:beginIdx]...)
	for _, line := range psBlockLines() {
		out = append(out, line+"\r")
	}
	out = append(out, lines[endIdx+1:]...)
	if err := os.WriteFile(profile, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		Fail("internal", "写入 $PROFILE 失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{"installed": true, "already": false, "upgraded": true, "profile": profile})
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
