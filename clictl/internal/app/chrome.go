package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"clictl/internal/api"
	"clictl/internal/appinfo"

	"golang.org/x/sys/windows/registry"
)

const (
	// chromePathEnv 显式指定 chrome.exe 的环境变量。设置后不再回退：
	// 指向的路径不存在直接报错，避免用户以为切了渠道却静默跑在别处。
	chromePathEnv = "CLICTL_CHROME_PATH"

	// chromeAppsRoot 所有项目共享的 Chrome 应用模式 profile 根目录名。
	// 各项目必须逐字一致（禁止加项目名前缀）：一旦分叉，多开就会裂成
	// 多个浏览器进程，共享进程的收益归零。
	chromeAppsRoot = "chrome-apps"
)

// LocateChrome 按优先级定位 chrome.exe：显式覆盖 → 注册表 App Paths → 常见安装路径。
func LocateChrome(override string) (string, error) {
	if override != "" {
		if !isChromeExecutable(override) {
			return "", fmt.Errorf("%s 指向的文件不存在: %s", chromePathEnv, override)
		}
		return override, nil
	}
	if path := chromeFromRegistry(); path != "" {
		return path, nil
	}
	if path := chromeFromCommonPaths(); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("未找到 chrome.exe，请安装 Chrome 或用 %s 显式指定", chromePathEnv)
}

// chromeFromRegistry 读 App Paths 注册项（HKLM 优先，其次 HKCU）。
func chromeFromRegistry() string {
	const appPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		key, err := registry.OpenKey(root, appPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		path, _, err := key.GetStringValue("")
		_ = key.Close()
		if err == nil && isChromeExecutable(path) {
			return path
		}
	}
	return ""
}

// chromeFromCommonPaths 覆盖未写注册表的绿色版 / 非默认盘安装位置。
func chromeFromCommonPaths() string {
	var candidates []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
	}
	for _, path := range candidates {
		if isChromeExecutable(path) {
			return path
		}
	}
	return ""
}

func isChromeExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ChromeProfileDir 返回共享专用 profile 目录（所有项目、同一通道共用）：
// %LOCALAPPDATA%\language_projects\chrome-apps\<dev|prod>，取不到回退 ~/.language_projects。
//
// dev / prod 分层保证调试实例与常驻正式版互不干扰；profile 含大量浏览器缓存，
// 因此放在 LocalAppData（不漫游）而非 %APPDATA%。
func ChromeProfileDir() (string, error) {
	channel := appinfo.Channel()
	if base, err := os.UserCacheDir(); err == nil && base != "" {
		return ensureChromeProfileDir(filepath.Join(base, "language_projects", chromeAppsRoot, channel))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return ensureChromeProfileDir(filepath.Join(home, ".language_projects", chromeAppsRoot, channel))
	}
	return "", fmt.Errorf("无法确定 Chrome 共享 profile 目录")
}

func ensureChromeProfileDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 Chrome 共享 profile 目录失败: %w", err)
	}
	return dir, nil
}

// openChromeAppWindow 用共享 profile 的 Chrome 应用模式打开 WebUI。
// 定位 / 建目录 / 拉起任一失败都落盘日志并回退系统默认浏览器，
// 保证「窗口一定打得开」，只是可能不是 chrome 形态。
func openChromeAppWindow(url string) {
	if err := startChromeAppWindow(url); err != nil {
		appinfo.Logf("Chrome 应用模式启动失败，回退默认浏览器: %v", err)
		if openErr := api.OpenBrowser(url); openErr != nil {
			appinfo.Logf("回退默认浏览器也失败: %v", openErr)
		}
	}
}

func startChromeAppWindow(url string) error {
	exe, err := LocateChrome(os.Getenv(chromePathEnv))
	if err != nil {
		return err
	}
	profile, err := ChromeProfileDir()
	if err != nil {
		return err
	}

	// 参数保持最小集：这是多项目共享的完整浏览器，不做 WebView2 式的激进剪枝。
	cmd := exec.Command(exe,
		"--app="+url,
		"--user-data-dir="+profile,
		"--no-first-run",
		"--no-default-browser-check",
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起 chrome 失败: %w", err)
	}

	// 共享 profile 已有 chrome 进程时，本次进程只是把请求转交后立即退出，
	// 因此只能异步回收，不能阻塞调用方。
	go func() { _ = cmd.Wait() }()

	appinfo.Logf("Chrome 应用模式窗口已拉起: %s（profile: %s）", url, profile)
	return nil
}
