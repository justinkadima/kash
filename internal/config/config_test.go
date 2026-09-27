package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsOnMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != Default().BaseURL {
		t.Errorf("BaseURL = %q, want default", cfg.BaseURL)
	}
	if cfg.Model != "" {
		t.Errorf("Model = %q, want empty", cfg.Model)
	}
}

func TestLoadOverridesAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte(`{"base_url": "http://x/","model":"m","temperature": 9, "chat_ratio": 2}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://x" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Temperature != 2 {
		t.Errorf("Temperature = %v, want clamped 2", cfg.Temperature)
	}
	if cfg.ChatRatio != 0.7 {
		t.Errorf("ChatRatio = %v, want clamped 0.7", cfg.ChatRatio)
	}
	if cfg.Model != "m" {
		t.Errorf("Model = %q", cfg.Model)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg := Default()
	cfg.Model = "llama3.1:8b"
	cfg.ContextLines = 33
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "llama3.1:8b" || got.ContextLines != 33 {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestPathOverride(t *testing.T) {
	t.Setenv("CON_CONFIG", "")
	p, err := Path("/custom/cfg.json")
	if err != nil || p != "/custom/cfg.json" {
		t.Errorf("Path(override) = %q, %v", p, err)
	}
	t.Setenv("CON_CONFIG", "/env/cfg.json")
	p, err = Path("")
	if err != nil || p != "/env/cfg.json" {
		t.Errorf("Path(env) = %q, %v", p, err)
	}
}
