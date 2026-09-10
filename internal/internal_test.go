package internal

import (
	"strings"
	"testing"
)

// PowerOff and PowerCycle pick the gentlest transition the device advertises.
func TestSelectState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wanted    []PowerState
		available []PowerState
		want      PowerState
	}{
		{
			name:      "prefers the gentlest available",
			wanted:    powerOffStates(),
			available: []PowerState{PowerOffHard, PowerOffSoft, PowerOffSoftGraceful},
			want:      PowerOffSoftGraceful,
		},
		{
			name:      "falls back to a harsher transition",
			wanted:    powerOffStates(),
			available: []PowerState{PowerOffHard},
			want:      PowerOffHard,
		},
		{
			// The real device under test advertises exactly this set.
			name:      "matches a real device's advertised states",
			wanted:    powerOffStates(),
			available: []PowerState{PowerMasterBusReset, PowerOffSoft, PowerCycleOffSoft, PowerDiagnosticInterrupt},
			want:      PowerOffSoft,
		},
		{
			name:      "no overlap yields unknown",
			wanted:    powerOffStates(),
			available: []PowerState{PowerSleepLight},
			want:      PowerUnknown,
		},
		{
			name:      "empty available yields unknown",
			wanted:    powerOffStates(),
			available: nil,
			want:      PowerUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := selectState(tc.wanted, tc.available); got != tc.want {
				t.Errorf("selectState() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNormalizeGUID(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"1B72A5CE45849E6A9F1288AEDD753DA0": "1b72a5ce-4584-9e6a-9f12-88aedd753da0",
		"1b72a5ce45849e6a9f1288aedd753da0": "1b72a5ce-4584-9e6a-9f12-88aedd753da0",
		"short":                            "short",
		"":                                 "",
	}
	for in, want := range tests {
		if got := normalizeGUID(in); got != want {
			t.Errorf("normalizeGUID(%q) = %q, want %q", in, got, want)
		}
	}
}

// http:// is the dangerous case: AMT accepts every call in the boot sequence
// and then silently boots normally.
func TestValidateImageURL(t *testing.T) {
	t.Parallel()

	for _, u := range []string{
		"https://images.example.com/hook.iso",
		"https://10.0.0.10:8443/a/b.iso",
	} {
		if err := validateImageURL(u); err != nil {
			t.Errorf("validateImageURL(%q) = %v, want nil", u, err)
		}
	}

	for _, u := range []string{
		"", "http://images.example.com/hook.iso",
		"ftp://images.example.com/hook.iso", "https://", "/relative/path.iso",
	} {
		if err := validateImageURL(u); err == nil {
			t.Errorf("validateImageURL(%q) = nil, want an error", u)
		}
	}
}

func TestBootTargetsMapped(t *testing.T) {
	t.Parallel()

	for _, target := range []BootTarget{BootPxe, BootHdd, BootCd, BootUefiHTTP} {
		if _, ok := bootSources[target]; !ok {
			t.Errorf("boot target %q has no CIM source mapping", target)
		}
	}
	// BiosSetup goes through BootSettingData rather than a boot source.
	if _, ok := bootSources[BootBiosSetup]; ok {
		t.Error("BiosSetup should not have a boot source mapping")
	}
}

func TestIsZeroMAC(t *testing.T) {
	t.Parallel()

	if !isZeroMAC("00:00:00:00:00:00") {
		t.Error("all-zero MAC should be reported as zero")
	}
	for _, mac := range []string{"88:ae:dd:75:3d:a0", "00:00:00:00:00:01", ""} {
		if isZeroMAC(mac) {
			t.Errorf("isZeroMAC(%q) = true, want false", mac)
		}
	}
}

func TestControlModeAndProvisioningStrings(t *testing.T) {
	t.Parallel()

	if got := controlModeString(2); got != ControlModeAdmin {
		t.Errorf("controlModeString(2) = %q", got)
	}
	if got := controlModeString(1); got != ControlModeClient {
		t.Errorf("controlModeString(1) = %q", got)
	}
	if got := controlModeString(0); got != ControlModeNotProvisioned {
		t.Errorf("controlModeString(0) = %q", got)
	}
	if got := controlModeString(9); !strings.HasPrefix(got, "Unknown") {
		t.Errorf("controlModeString(9) = %q, want an Unknown form", got)
	}
	if got := provisioningStateString(2); got != ProvisioningPost {
		t.Errorf("provisioningStateString(2) = %q", got)
	}
}
