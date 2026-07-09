package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSettings(t *testing.T) {
	good := defaultSettings()
	if err := validateSettings(good); err != nil {
		t.Fatalf("default settings should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"bad endpoint version", func(s *Settings) { s.EndpointVersion = "ipv4" }},
		{"empty endpoint version", func(s *Settings) { s.EndpointVersion = "" }},
		{"port zero", func(s *Settings) { s.SocksPort = 0 }},
		{"port too big", func(s *Settings) { s.SocksPort = 70000 }},
		{"connect port zero", func(s *Settings) { s.ConnectPort = 0 }},
		{"bad dns", func(s *Settings) { s.DNSServers = []string{"9.9.9.9", "not-an-ip"} }},
		{"dns shell injection", func(s *Settings) { s.DNSServers = []string{"9.9.9.9; rm -rf /"} }},
		{"bad device name", func(s *Settings) { s.DeviceName = "evil name$(reboot)" }},
		{"device name too long", func(s *Settings) { s.DeviceName = strings.Repeat("a", 65) }},
	}
	for _, tc := range cases {
		s := defaultSettings()
		tc.mutate(&s)
		if err := validateSettings(s); err == nil {
			t.Errorf("%s: expected validation error, got nil", tc.name)
		}
	}

	ok := defaultSettings()
	ok.EndpointVersion = "v6"
	ok.SocksPort = 1080
	ok.DNSServers = []string{"1.1.1.1", "2620:fe::fe"}
	ok.DeviceName = "my-router_01.lan"
	if err := validateSettings(ok); err != nil {
		t.Errorf("valid settings rejected: %v", err)
	}
}

func TestValidateJWT(t *testing.T) {
	if err := validateJWT(""); err != nil {
		t.Errorf("empty jwt should be allowed (no zero-trust enrollment)")
	}
	if err := validateJWT("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.sig-part_1"); err != nil {
		t.Errorf("well-formed jwt rejected: %v", err)
	}
	for _, bad := range []string{"a b", "tok;en", "$(cmd)", "tok\nen", strings.Repeat("a", 5000)} {
		if err := validateJWT(bad); err == nil {
			t.Errorf("jwt %q should be rejected", bad)
		}
	}
}

func TestSettingsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	oldSettings := SettingsFile
	SettingsFile = filepath.Join(dir, "settings.json")
	defer func() { SettingsFile = oldSettings }()

	// missing file -> defaults
	got := loadSettings()
	if !reflect.DeepEqual(got, defaultSettings()) {
		t.Fatalf("expected defaults for missing file, got %+v", got)
	}

	want := Settings{
		EndpointVersion: "v6",
		SocksPort:       1085,
		ConnectPort:     4443,
		DNSServers:      []string{"1.1.1.1"},
		DeviceName:      "unit-test",
	}
	if err := saveSettings(want); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}

	// file mode must be 0600
	fi, err := os.Stat(SettingsFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("settings file mode = %o, want 0600", fi.Mode().Perm())
	}

	// corrupted file -> defaults, not a crash
	if err := os.WriteFile(SettingsFile, []byte("{nope"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(); !reflect.DeepEqual(got, defaultSettings()) {
		t.Fatalf("expected defaults for corrupt file, got %+v", got)
	}
}

func TestWarpConfigRedaction(t *testing.T) {
	wc := WarpConfig{
		PrivateKey:  "SUPERSECRETKEY",
		AccessToken: "SECRETTOKEN",
		License:     "SECRETLICENSE",
		ID:          "device-id-1",
		EndpointV4:  "162.159.198.1",
		EndpointV6:  "2606:4700:103::1",
		IPv4:        "172.16.0.2",
		IPv6:        "2606:4700:110:8888::1",
	}
	red := redactWarpConfig(wc)

	for k, v := range red {
		if s, ok := v.(string); ok {
			for _, secret := range []string{"SUPERSECRETKEY", "SECRETTOKEN", "SECRETLICENSE"} {
				if strings.Contains(s, secret) {
					t.Errorf("redacted output leaks secret in key %s", k)
				}
			}
		}
	}
	if red["DeviceID"] != "device-id-1" {
		t.Errorf("DeviceID missing from redacted config")
	}
	if red["HasPrivateKey"] != true || red["HasAccessToken"] != true || red["HasLicense"] != true {
		t.Errorf("secret presence booleans wrong: %+v", red)
	}
}

func TestWarpRegistered(t *testing.T) {
	dir := t.TempDir()
	oldWarp := WarpConfigFile
	WarpConfigFile = filepath.Join(dir, "config.json")
	defer func() { WarpConfigFile = oldWarp }()

	if warpRegistered() {
		t.Fatal("missing config should not count as registered")
	}
	os.WriteFile(WarpConfigFile, []byte(`{"private_key":"","id":""}`), 0600)
	if warpRegistered() {
		t.Fatal("empty credentials should not count as registered")
	}
	os.WriteFile(WarpConfigFile, []byte(`{"private_key":"abc","id":"dev1"}`), 0600)
	if !warpRegistered() {
		t.Fatal("valid credentials should count as registered")
	}
}

func TestBuildSocksArgs(t *testing.T) {
	s := Settings{
		EndpointVersion: "v4",
		SocksPort:       1080,
		ConnectPort:     443,
		DNSServers:      []string{},
	}
	got := buildSocksArgs(s, "172.18.0.2")
	want := []string{"-c", WarpConfigFile, "socks", "-b", "172.18.0.2", "-p", "1080"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("v4 default args: got %v want %v", got, want)
	}

	s = Settings{
		EndpointVersion: "v6",
		SocksPort:       1085,
		ConnectPort:     4443,
		DNSServers:      []string{"1.1.1.1", "9.9.9.9"},
	}
	got = buildSocksArgs(s, "172.18.0.2")
	want = []string{
		"-c", WarpConfigFile, "socks", "-b", "172.18.0.2", "-p", "1085",
		"-P", "4443", "-6", "-d", "1.1.1.1", "-d", "9.9.9.9",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("v6 custom args: got %v want %v", got, want)
	}
}

func TestBuildRegisterArgs(t *testing.T) {
	got := buildRegisterArgs("", "")
	want := []string{"-c", WarpConfigFile, "register", "--accept-tos"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bare register args: got %v want %v", got, want)
	}

	got = buildRegisterArgs("spr-router", "eyJ.token.sig")
	want = []string{"-c", WarpConfigFile, "register", "--accept-tos",
		"--name", "spr-router", "--jwt", "eyJ.token.sig"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full register args: got %v want %v", got, want)
	}
}

func TestParseTrace(t *testing.T) {
	text := "fl=123abc\nip=104.28.200.1\ncolo=AMS\nwarp=on\n\nnot a pair\n"
	got := parseTrace(text)
	if got["warp"] != "on" || got["colo"] != "AMS" || got["ip"] != "104.28.200.1" {
		t.Errorf("parseTrace wrong: %+v", got)
	}
	if _, ok := got["not a pair"]; ok {
		t.Errorf("non key=value line should be skipped")
	}
	if len(parseTrace("")) != 0 {
		t.Errorf("empty trace should parse to empty map")
	}
}
