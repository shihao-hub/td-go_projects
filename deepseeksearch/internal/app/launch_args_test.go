package app

import "testing"

// IsSilentStart 是自启与单实例唤醒共用的单一事实源：
// 判错的两个方向分别是"误判 exe 路径内的 --hidden 字样"（回不去静默）
// 与"漏判真实 --hidden/--tray"（自启时炸出主窗口），两条路都必须锁死。
func TestIsSilentStart(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"空参数表", []string{}, false},
		{"仅可执行路径", []string{"C:\\apps\\deepseeksearch-gui.exe"}, false},
		{"exe 路径含 --hidden 字样不误判", []string{"D:\\apps\\--hidden\\deepseeksearch-gui.exe"}, false},
		{"exe 路径含 --tray 字样不误判", []string{"D:\\apps\\--tray\\deepseeksearch-gui.exe"}, false},
		{"开机自启 --hidden", []string{"C:\\apps\\gui.exe", "--hidden"}, true},
		{"别名 --tray", []string{"C:\\apps\\gui.exe", "--tray"}, true},
		{"大小写混合", []string{"C:\\apps\\gui.exe", "--HIDDEN"}, true},
		{"前后空白", []string{"C:\\apps\\gui.exe", "  --Hidden  "}, true},
		{"空白字符串不命中", []string{"C:\\apps\\gui.exe", "   "}, false},
		{"普通参数不命中", []string{"C:\\apps\\gui.exe", "--port", "8080"}, false},
		{"命中出现在后续参数", []string{"C:\\apps\\gui.exe", "--port", "8080", "--hidden"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSilentStart(tc.args); got != tc.want {
				t.Errorf("IsSilentStart(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
