package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFrom_Missing(t *testing.T) {
	c, err := loadFrom(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if c == nil || c.Provider == nil {
		t.Fatalf("expected non-nil Config with empty Provider map; got %+v", c)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	in := &Config{
		Defaults: DefaultsConfig{
			Provider:      "openai",
			MaxTurns:      25,
			ScriptTimeout: 30 * time.Second,
			ScriptMemMiB:  128,
			LogLevel:      "debug",
		},
		Provider: map[string]ProviderConfig{
			"anthropic": {Model: "claude-opus-4-7"},
			"openai":    {Model: "gpt-5", BaseURL: "https://api.openai.com"},
			"bedrock":   {Region: "us-west-2"},
		},
	}
	if err := saveTo(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := loadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.Defaults != in.Defaults {
		t.Errorf("defaults mismatch: %+v vs %+v", out.Defaults, in.Defaults)
	}
	for k, want := range in.Provider {
		if got := out.Provider[k]; got != want {
			t.Errorf("provider %q mismatch: got %+v want %+v", k, got, want)
		}
	}
}

func TestLoadFrom_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := writeFile(t, path, "not valid toml = = ="); err != nil {
		t.Fatal(err)
	}
	_, err := loadFrom(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected error to mention parse; got: %v", err)
	}
}

func TestProviderOf(t *testing.T) {
	c := &Config{
		Provider: map[string]ProviderConfig{
			"anthropic": {Model: "claude-foo"},
		},
	}
	if got := c.ProviderOf("anthropic").Model; got != "claude-foo" {
		t.Errorf("expected claude-foo; got %q", got)
	}
	if got := c.ProviderOf("missing"); got != (ProviderConfig{}) {
		t.Errorf("expected zero ProviderConfig; got %+v", got)
	}
	var nilC *Config
	if got := nilC.ProviderOf("anything"); got != (ProviderConfig{}) {
		t.Errorf("nil receiver should return zero; got %+v", got)
	}
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0o600)
}
