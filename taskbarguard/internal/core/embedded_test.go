package core

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMaterializeEmbeddedScripts(t *testing.T) {
	dataDir := t.TempDir()

	dir, err := MaterializeEmbeddedScripts(dataDir)
	if err != nil {
		t.Fatalf("释放内嵌脚本失败: %v", err)
	}
	expected := filepath.Join(dataDir, "scripts")
	if dir != expected {
		t.Fatalf("期望脚本目录 %q，实际 %q", expected, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "wt")); err != nil {
		t.Fatalf("缺少应用目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, embeddedHashName)); err != nil {
		t.Fatalf("缺少指纹文件: %v", err)
	}
}

func TestMaterializeEmbeddedScriptsReusesMatchingTree(t *testing.T) {
	dataDir := t.TempDir()

	dir, err := MaterializeEmbeddedScripts(dataDir)
	if err != nil {
		t.Fatalf("首次释放失败: %v", err)
	}
	marker := filepath.Join(dir, "wt", "marker.txt")
	if err := os.WriteFile(marker, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := MaterializeEmbeddedScripts(dataDir); err != nil {
		t.Fatalf("二次释放失败: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "kept" {
		t.Fatalf("指纹一致时应复用现有目录，实际 data=%q err=%v", data, err)
	}
}

func TestHashFSStable(t *testing.T) {
	tree := fstest.MapFS{
		"wt/patch_wt.py":             {Data: []byte("print(1)")},
		"wt/custom_icons/a.ico":      {Data: []byte{0, 1, 2}},
		"deepseek_harness/readme.md": {Data: []byte("x")},
	}
	first, err := hashFS(tree)
	if err != nil {
		t.Fatalf("计算指纹失败: %v", err)
	}
	second, err := hashFS(tree)
	if err != nil {
		t.Fatalf("计算指纹失败: %v", err)
	}
	if first != second {
		t.Fatalf("同一文件树指纹应稳定: %s != %s", first, second)
	}
}
