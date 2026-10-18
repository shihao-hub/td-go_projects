package engine

import (
	"testing"
)

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		raw      string
		wantExe  string
		wantArgs string
	}{
		{
			raw:      `"C:\Program Files\App\app.exe" --startup /min`,
			wantExe:  `C:\Program Files\App\app.exe`,
			wantArgs: `--startup /min`,
		},
		{
			raw:      `C:\Windows\System32\cmd.exe /c echo hello`,
			wantExe:  `C:\Windows\System32\cmd.exe`,
			wantArgs: `/c echo hello`,
		},
		{
			raw:      `C:\Program Files\Some App\tool.exe -s`,
			wantExe:  `C:\Program Files\Some App\tool.exe`,
			wantArgs: `-s`,
		},
		{
			raw:      `"D:\MyApp.exe"`,
			wantExe:  `D:\MyApp.exe`,
			wantArgs: ``,
		},
		{
			raw:      `notepad.exe`,
			wantExe:  `notepad.exe`,
			wantArgs: ``,
		},
	}

	for _, tt := range tests {
		exe, args := ParseCommandLine(tt.raw)
		if exe != tt.wantExe || args != tt.wantArgs {
			t.Errorf("ParseCommandLine(%q) = (%q, %q), want (%q, %q)", tt.raw, exe, args, tt.wantExe, tt.wantArgs)
		}
	}
}