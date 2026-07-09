package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var TEST_PREFIX = os.Getenv("TEST_PREFIX")

// Plugin settings (non-secret) live in settings.json. The WARP credentials
// written by `usque register` live in config.json (0600, never returned to
// the UI unredacted).
var SettingsFile = TEST_PREFIX + "/configs/spr-masque/settings.json"
var WarpConfigFile = TEST_PREFIX + "/configs/spr-masque/config.json"

var settingsMtx sync.RWMutex

// Settings is the plugin configuration exposed over GET/PUT /config.
type Settings struct {
	// EndpointVersion selects the Cloudflare MASQUE endpoint family: "v4" or "v6".
	EndpointVersion string
	// SocksPort is the SOCKS5 listen port on the container IP (default 1080).
	SocksPort int
	// ConnectPort is the UDP port used for the MASQUE connection (default 443).
	ConnectPort int
	// DNSServers are resolvers used inside the tunnel stack by usque.
	// Empty means usque's defaults (Quad9).
	DNSServers []string
	// DeviceName is the WARP device name used at registration time.
	DeviceName string
}

func defaultSettings() Settings {
	return Settings{
		EndpointVersion: "v4",
		SocksPort:       1080,
		ConnectPort:     443,
		DNSServers:      []string{},
		DeviceName:      "spr-masque",
	}
}

var deviceNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// JWTs are base64url segments joined with dots (length checked separately;
// Go's regexp caps repeat counts at 1000).
var jwtRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validateDeviceName(name string) error {
	if name == "" {
		return nil
	}
	if !deviceNameRe.MatchString(name) {
		return fmt.Errorf("invalid device name: only [A-Za-z0-9._-], max 64 chars")
	}
	return nil
}

func validateJWT(jwt string) error {
	if jwt == "" {
		return nil
	}
	if len(jwt) > 4096 || !jwtRe.MatchString(jwt) {
		return fmt.Errorf("invalid enrollment token format")
	}
	return nil
}

func validateSettings(s Settings) error {
	if s.EndpointVersion != "v4" && s.EndpointVersion != "v6" {
		return fmt.Errorf("EndpointVersion must be \"v4\" or \"v6\"")
	}
	if s.SocksPort < 1 || s.SocksPort > 65535 {
		return fmt.Errorf("SocksPort must be between 1 and 65535")
	}
	if s.ConnectPort < 1 || s.ConnectPort > 65535 {
		return fmt.Errorf("ConnectPort must be between 1 and 65535")
	}
	for _, dns := range s.DNSServers {
		if net.ParseIP(dns) == nil {
			return fmt.Errorf("invalid DNS server IP: %q", dns)
		}
	}
	return validateDeviceName(s.DeviceName)
}

func loadSettings() Settings {
	settingsMtx.RLock()
	defer settingsMtx.RUnlock()

	s := defaultSettings()
	data, err := os.ReadFile(SettingsFile)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Println("[-] failed to parse settings, using defaults:", err)
		return defaultSettings()
	}
	if s.DNSServers == nil {
		s.DNSServers = []string{}
	}
	return s
}

// saveSettings writes settings atomically (tmp+rename, 0600).
func saveSettings(s Settings) error {
	settingsMtx.Lock()
	defer settingsMtx.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := SettingsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, SettingsFile)
}

// WarpConfig mirrors usque's config.json layout. Key material and tokens are
// confidential and must never leave the backend.
type WarpConfig struct {
	PrivateKey     string `json:"private_key"`
	EndpointV4     string `json:"endpoint_v4"`
	EndpointV6     string `json:"endpoint_v6"`
	EndpointH2V4   string `json:"endpoint_h2_v4"`
	EndpointH2V6   string `json:"endpoint_h2_v6"`
	EndpointPubKey string `json:"endpoint_pub_key"`
	License        string `json:"license"`
	ID             string `json:"id"`
	AccessToken    string `json:"access_token"`
	IPv4           string `json:"ipv4"`
	IPv6           string `json:"ipv6"`
}

func loadWarpConfig() (WarpConfig, error) {
	wc := WarpConfig{}
	data, err := os.ReadFile(WarpConfigFile)
	if err != nil {
		return wc, err
	}
	err = json.Unmarshal(data, &wc)
	return wc, err
}

func warpRegistered() bool {
	wc, err := loadWarpConfig()
	return err == nil && wc.PrivateKey != "" && wc.ID != ""
}

// redactWarpConfig returns only the non-secret registration facts, safe for
// the UI. Secrets are reported as booleans.
func redactWarpConfig(wc WarpConfig) map[string]interface{} {
	return map[string]interface{}{
		"DeviceID":       wc.ID,
		"EndpointV4":     wc.EndpointV4,
		"EndpointV6":     wc.EndpointV6,
		"WarpIPv4":       wc.IPv4,
		"WarpIPv6":       wc.IPv6,
		"HasPrivateKey":  wc.PrivateKey != "",
		"HasAccessToken": wc.AccessToken != "",
		"HasLicense":     wc.License != "",
	}
}

func ensureConfigDir() error {
	return os.MkdirAll(filepath.Dir(SettingsFile), 0700)
}
