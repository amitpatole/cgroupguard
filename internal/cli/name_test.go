package cli

import "testing"

func TestCommandNameFollowsTheShortAlias(t *testing.T) {
	for in, want := range map[string]string{
		"cgroupguard": "cgroupguard", "/usr/bin/cgroupguard": "cgroupguard", "./cgroupguard-bin": "cgroupguard",
		"cgg": "cgg", "/usr/bin/cgg": "cgg", `C:\\tools\\cgg.exe`: "cgg", "CGG.EXE": "cgg", "cggx": "cgroupguard", "": "cgroupguard",
	} {
		if got := commandName(in); got != want {
			t.Errorf("commandName(%q) = %q, want %q", in, got, want)
		}
	}
}
