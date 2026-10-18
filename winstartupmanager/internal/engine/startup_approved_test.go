package engine

import (
	"testing"
)

func TestParseStartupApprovedBytes(t *testing.T) {
	tests := []struct {
		data []byte
		want bool
	}{
		{data: nil, want: true},
		{data: []byte{}, want: true},
		{data: []byte{0x02, 0x00, 0x00, 0x00}, want: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00}, want: true},
		{data: []byte{0x06, 0x00, 0x00, 0x00}, want: true},
		{data: []byte{0x03, 0x00, 0x00, 0x00}, want: false},
		{data: []byte{0x01, 0x00, 0x00, 0x00}, want: false},
	}

	for _, tt := range tests {
		got := ParseStartupApprovedBytes(tt.data)
		if got != tt.want {
			t.Errorf("ParseStartupApprovedBytes(%v) = %v, want %v", tt.data, got, tt.want)
		}
	}
}