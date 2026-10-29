//go:build !windows

package app

func guiSingleInstanceID(release bool) string {
	id := "shihao.langproj.deepseeksearch.gui"
	if !release {
		id += ".dev"
	}
	return id
}

func GUIRunning(release bool) bool {
	return false
}
