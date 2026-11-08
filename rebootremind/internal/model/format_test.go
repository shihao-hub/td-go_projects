package model

import "testing"

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		seconds int64
		want    string
	}{
		{0, "0 分"},
		{59, "0 分"},
		{60, "1 分"},
		{3661, "1 小时 1 分"},
		{86400 + 3600*14, "1 天 14 小时"},
		{-10, "0 分"},
	}
	for _, test := range cases {
		if got := FormatUptime(test.seconds); got != test.want {
			t.Fatalf("FormatUptime(%d) = %q, want %q", test.seconds, got, test.want)
		}
	}
}
