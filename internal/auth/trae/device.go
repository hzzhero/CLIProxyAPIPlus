package trae

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Device key pair (P-256 ECDSA)
// ---------------------------------------------------------------------------

// DeviceKeyPair holds a freshly-generated P-256 ECDSA device key pair
// as PEM-encoded strings. The public key is embedded inside DeviceInfo
// (in SPKI form) so the Trae server can bind the session to this
// device; the private key is required later when refreshing with
// DeviceProof (P-256 SHA-256 ASN.1 signature over the standardized
// refresh message).
type DeviceKeyPair struct {
	PrivateKeyPEM string `json:"privateKeyPEM"`
	PublicKeyPEM  string `json:"publicKeyPEM"`
}

// GenerateDeviceKeyPair creates a new P-256 ECDSA key pair and returns
// it in PEM form. Mirror of cockpit-tools' generate_device_key_pair.
func GenerateDeviceKeyPair() (*DeviceKeyPair, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("trae device: generate P-256 key failed: %w", err)
	}
	privPKCS8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("trae device: marshal PKCS#8 private key failed: %w", err)
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privPKCS8,
	}))
	pubSPKI, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("trae device: marshal SPKI public key failed: %w", err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubSPKI,
	}))
	return &DeviceKeyPair{PrivateKeyPEM: privPEM, PublicKeyPEM: pubPEM}, nil
}

// ---------------------------------------------------------------------------
// Numeric device id generator
// ---------------------------------------------------------------------------

// DefaultNumericDeviceID returns a fresh pseudo-numeric device id that
// satisfies the 8..24 digit Trae constraint (normalize_device_id).
// It uses crypto/rand so consecutive runs do not collide.
func DefaultNumericDeviceID() string {
	// Match cockpit-tools' typical 18..19-digit real IDE device ids.
	const digits = 19
	// The first digit is in range [1..9] so the id never has a leading
	// zero (matching real IDE ids like "7633793279305631249").
	first, err := rand.Int(rand.Reader, big.NewInt(9))
	if err != nil {
		// crypto/rand failure on any realistic system is unrecoverable;
		// fall back to a static prefix + time so downstream still sees
		// a valid numeric id.
		return "1" + strings.Repeat("0", digits-1)
	}
	var sb strings.Builder
	sb.Grow(digits)
	sb.WriteByte(byte('1' + first.Int64()))
	for i := 1; i < digits; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			sb.WriteByte('0')
			continue
		}
		sb.WriteByte(byte('0' + n.Int64()))
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Official DeviceInfo object (matches build_official_device_info)
// ---------------------------------------------------------------------------

// DevicePlatformCode selects SOLO_PC vs IDE_PC depending on whether
// the login is for TRAE SOLO.
func DevicePlatformCode(isSolo bool) string {
	if isSolo {
		return "SOLO_PC"
	}
	return "IDE_PC"
}

// BuildOfficialDeviceInfo assembles the DeviceInfo JSON payload that
// ExchangeToken requires since the server introduced device-proof
// validation. The public key comes from GenerateDeviceKeyPair.
func BuildOfficialDeviceInfo(dc DeviceContext, isSolo bool, publicKeyPEM string) map[string]any {
	return map[string]any{
		"DeviceID":        dc.DeviceID,
		"MachineID":       dc.MachineID,
		"PlatformCode":    DevicePlatformCode(isSolo),
		"DeviceType":      "PC",
		"DeviceName":      deviceDisplayName(),
		"DeviceModel":     dc.XDeviceBrand,
		"ClientVersion":   dc.XAppVersion,
		"DevicePublicKey": publicKeyPEM,
		"DeviceBrand":     deviceBrandForContext(dc),
		"DeviceCPU":       "",
		"OSInfo":          dc.XDeviceType,
		"OSVersion":       dc.XOSVersion,
	}
}

// deviceDisplayName returns a short, safe host label shown in the
// DeviceInfo.DeviceName field. It never returns an empty string so
// ExchangeToken validation cannot complain.
func deviceDisplayName() string {
	if hostname, err := os.Hostname(); err == nil {
		if trimmed := strings.TrimSpace(hostname); trimmed != "" {
			return trimmed
		}
	}
	for _, env := range []string{"USER", "USERNAME", "HOSTNAME", "COMPUTERNAME"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return "PC"
}

func deviceBrandForContext(dc DeviceContext) string {
	switch strings.ToLower(dc.XDeviceType) {
	case "darwin", "mac", "macos":
		return "Apple"
	case "windows":
		return "Microsoft"
	case "linux":
		return "Linux"
	}
	if dc.XDeviceBrand != "" {
		return dc.XDeviceBrand
	}
	return "PC"
}

// ---------------------------------------------------------------------------
// Platform / OS helpers reused by both DefaultDeviceContext and tests
// ---------------------------------------------------------------------------

// DefaultXDeviceType mirrors cockpit-tools' detect_device_type.
func DefaultXDeviceType() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	}
	return "unknown"
}

// DefaultXDeviceBrand mirrors cockpit-tools' detect_device_brand fallback.
func DefaultXDeviceBrand() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return "PC"
}

// DefaultXOSVersion mirrors cockpit-tools' detect_os_version.
func DefaultXOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return "unknown"
}
