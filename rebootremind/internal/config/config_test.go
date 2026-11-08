package config

import "testing"

func TestDefaultValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("默认配置应合法: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"开机下限为零", func(c *Config) { c.MinUptimeHours = 0 }},
		{"内存水位超上限", func(c *Config) { c.MinMemoryPercent = 101 }},
		{"内存水位为零", func(c *Config) { c.MinMemoryPercent = 0 }},
		{"强制时长短于提醒时长", func(c *Config) { c.ForceUptimeHours = 10 }},
		{"短延迟为零", func(c *Config) { c.SnoozeShortHours = 0 }},
		{"长延迟短于短延迟", func(c *Config) { c.SnoozeLongHours = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Default()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
		})
	}
}
