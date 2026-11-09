package engine

import "testing"

func TestCollectibleTrigger(t *testing.T) {
	yes := map[int]bool{
		TASK_TRIGGER_TIME: true, TASK_TRIGGER_DAILY: true, TASK_TRIGGER_WEEKLY: true,
		TASK_TRIGGER_MONTHLY: true, TASK_TRIGGER_MONTHLYDOW: true,
		TASK_TRIGGER_BOOT: true, TASK_TRIGGER_LOGON: true,
	}
	no := []int{0, 6, 7, 10, 11, 99}
	for v := range yes {
		if !collectibleTrigger(v) {
			t.Errorf("触发器 %d 应纳入采集", v)
		}
	}
	for _, v := range no {
		if collectibleTrigger(v) {
			t.Errorf("触发器 %d 不应纳入采集", v)
		}
	}
}

func TestIntervalText(t *testing.T) {
	cases := map[string]string{
		"PT1H":  "每小时",
		"pt1h":  "每小时",
		"PT30M": "每 30 分钟",
		"PT90M": "每 90 分钟", // 非整小时/整天按分钟输出
		"P1D":   "每天",
		"PT24H": "每天",
		"P7D":   "每 7 天",
		"PT12H": "每 12 小时",
		"":      "",
	}
	for in, want := range cases {
		if got := intervalText(in); got != want {
			t.Errorf("intervalText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitTaskPath(t *testing.T) {
	folder, name, err := splitTaskPath(`\rebootremind_check`)
	if err != nil || folder != `\` || name != "rebootremind_check" {
		t.Errorf("根路径任务拆分错误: folder=%q name=%q err=%v", folder, name, err)
	}

	folder, name, err = splitTaskPath(`\dir\name`)
	if err != nil || folder != `\dir` || name != "name" {
		t.Errorf("子目录任务拆分错误: folder=%q name=%q err=%v", folder, name, err)
	}

	folder, name, err = splitTaskPath("name")
	if err != nil || folder != `\` || name != "name" {
		t.Errorf("裸名拆分错误: folder=%q name=%q err=%v", folder, name, err)
	}

	if _, _, err := splitTaskPath("  "); err == nil {
		t.Error("空任务路径应报错")
	}
}
