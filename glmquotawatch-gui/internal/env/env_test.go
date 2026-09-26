package env

import "testing"

func TestEnvProperties(t *testing.T) {
	if IsDev() {
		if SingleInstanceID() != "shihao.langproj.glmquotawatch-gui.dev" {
			t.Errorf("unexpected dev single instance id: %s", SingleInstanceID())
		}
		if DataSubDir() != "dev" {
			t.Errorf("unexpected dev data sub dir: %s", DataSubDir())
		}
		if AutostartAllowed() {
			t.Errorf("dev should not allow autostart")
		}
	} else {
		if SingleInstanceID() != "shihao.langproj.glmquotawatch-gui" {
			t.Errorf("unexpected prod single instance id: %s", SingleInstanceID())
		}
		if DataSubDir() != "prod" {
			t.Errorf("unexpected prod data sub dir: %s", DataSubDir())
		}
		if !AutostartAllowed() {
			t.Errorf("prod should allow autostart")
		}
	}
}