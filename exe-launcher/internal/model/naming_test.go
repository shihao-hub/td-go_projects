package model

import "testing"

func TestExeBaseName(t *testing.T) {
	cases := map[string]string{
		`C:\a\b\Foo.exe`: "Foo",
		`C:\a\b\Foo.EXE`: "Foo",
		"bar.exe":        "bar",
		`D:\x\a.b.exe`:   "a.b",
	}
	for in, want := range cases {
		if got := ExeBaseName(in); got != want {
			t.Errorf("ExeBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}
