package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{Version: 2, Listener: Listener{Address: "127.0.0.1", Port: 8080}, Routes: []Route{{ID: "api", Name: "API", Enabled: true, Host: "api.test", Target: "http://127.0.0.1:3000"}}}
}
func TestSaveLoadYAMLRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", FileName)
	cfg := validConfig()
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Routes[0].Target != cfg.Routes[0].Target {
		t.Fatalf("target=%q", got.Routes[0].Target)
	}
}
func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), FileName)
	data := []byte("version: 2\nunknown: true\nlistener:\n  address: 127.0.0.1\n  port: 8080\nroutes: []\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted unknown field")
	}
}
func TestValidateReportsProblems(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Routes = append(cfg.Routes, Route{ID: "api", Name: "", Enabled: true, Host: "api.test", Target: ""})
	err := cfg.Validate()
	if err == nil {
		t.Fatal("nil error")
	}
	for _, want := range []string{"duplicate route id", "duplicate enabled host", "name is empty", "target: is empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q missing %q", err, want)
		}
	}
}
