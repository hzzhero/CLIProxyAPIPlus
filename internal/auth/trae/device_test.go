package trae

import (
	"regexp"
	"strings"
	"testing"
)

func TestDefaultNumericDeviceID_Format(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 50; i++ {
		id := DefaultNumericDeviceID()
		if len(id) < 8 || len(id) > 24 {
			t.Fatalf("len(id)=%d want 8..24 (id=%q)", len(id), id)
		}
		if !regexp.MustCompile(`^[1-9][0-9]{7,23}$`).MatchString(id) {
			t.Fatalf("id=%q does not match the 8..24 numeric-without-leading-zero rule", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q produced (crypto/rand should not collide)", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateDeviceKeyPair_PEMStructure(t *testing.T) {
	kp, err := GenerateDeviceKeyPair()
	if err != nil {
		t.Fatalf("GenerateDeviceKeyPair err=%v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(kp.PrivateKeyPEM), "-----BEGIN PRIVATE KEY-----") {
		t.Errorf("PrivateKeyPEM missing PEM header: %q", firstLine(kp.PrivateKeyPEM))
	}
	if !strings.HasSuffix(strings.TrimSpace(kp.PrivateKeyPEM), "-----END PRIVATE KEY-----") {
		t.Errorf("PrivateKeyPEM missing PEM footer")
	}
	if !strings.HasPrefix(strings.TrimSpace(kp.PublicKeyPEM), "-----BEGIN PUBLIC KEY-----") {
		t.Errorf("PublicKeyPEM missing PEM header: %q", firstLine(kp.PublicKeyPEM))
	}
	if !strings.HasSuffix(strings.TrimSpace(kp.PublicKeyPEM), "-----END PUBLIC KEY-----") {
		t.Errorf("PublicKeyPEM missing PEM footer")
	}

	// Two calls must produce different keys (sanity check for rand).
	kp2, _ := GenerateDeviceKeyPair()
	if kp.PublicKeyPEM == kp2.PublicKeyPEM {
		t.Errorf("two GenerateDeviceKeyPair produced identical public keys (rand broken?)")
	}
}

func TestBuildOfficialDeviceInfo_RequiredFields(t *testing.T) {
	dc := DefaultDeviceContext("test-client", CNEndpoints)
	kp, err := GenerateDeviceKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	info := BuildOfficialDeviceInfo(dc, false, kp.PublicKeyPEM)

	strField := func(k string) string {
		v, ok := info[k]
		if !ok {
			return ""
		}
		s, _ := v.(string)
		return s
	}

	if got := strField("DeviceID"); len(got) < 8 {
		t.Errorf("DeviceID=%q too short (want 8+ digits)", got)
	}
	if got := strField("MachineID"); got == "" {
		t.Error("MachineID empty")
	}
	if got := strField("PlatformCode"); got != "IDE_PC" {
		t.Errorf("PlatformCode=%q want IDE_PC", got)
	}
	if got := strField("DeviceType"); got != "PC" {
		t.Errorf("DeviceType=%q want PC", got)
	}
	if got := strField("DeviceName"); got == "" {
		t.Error("DeviceName empty")
	}
	if got := strField("ClientVersion"); got != CNEndpoints.DefaultAppVersion {
		t.Errorf("ClientVersion=%q want %q", got, CNEndpoints.DefaultAppVersion)
	}
	if got := strField("DevicePublicKey"); got != kp.PublicKeyPEM {
		t.Errorf("DevicePublicKey does not match supplied PEM")
	}
	if got := strField("OSInfo"); got == "" {
		t.Error("OSInfo empty")
	}
	if got := strField("OSVersion"); got == "" {
		t.Error("OSVersion empty")
	}

	solo := BuildOfficialDeviceInfo(dc, true, kp.PublicKeyPEM)
	if got, _ := solo["PlatformCode"].(string); got != "SOLO_PC" {
		t.Errorf("SOLO PlatformCode=%q want SOLO_PC", got)
	}
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
