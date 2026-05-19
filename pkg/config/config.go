// Package config implements the TOML-backed user config at
// ~/.autoswe/config.toml. It holds non-secret defaults — preferred
// provider, per-provider model, script sandbox bounds. Tokens never live
// here; see pkg/secrets for the keychain-backed token store.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// FileName is the file under ~/.autoswe that we read/write.
const FileName = "config.toml"

// Config is the on-disk shape. Add fields with omitempty + sensible
// zero values so a missing-file Load returns a usable zero Config.
type Config struct {
	Defaults DefaultsConfig            `toml:"defaults"`
	Provider map[string]ProviderConfig `toml:"provider"`
}

// DefaultsConfig holds settings that apply across providers.
type DefaultsConfig struct {
	Provider      string        `toml:"provider,omitempty"`
	MaxTurns      int           `toml:"max_turns,omitempty"`
	ScriptTimeout time.Duration `toml:"script_timeout,omitempty"`
	ScriptMemMiB  uint          `toml:"script_mem_mib,omitempty"`
	LogLevel      string        `toml:"log_level,omitempty"`
}

// ProviderConfig holds per-provider settings (model, region, ...).
type ProviderConfig struct {
	Model   string `toml:"model,omitempty"`
	Region  string `toml:"region,omitempty"`   // bedrock
	BaseURL string `toml:"base_url,omitempty"` // openai-compat / future
}

// Path returns the absolute config file path, ensuring the containing
// directory exists.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home: %w", err)
	}
	dir := filepath.Join(home, ".autoswe")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads the config file. A missing file is not an error — Load
// returns a zero-value Config in that case so callers can apply
// built-in defaults uniformly.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return loadFrom(p)
}

func loadFrom(path string) (*Config, error) {
	c := &Config{Provider: map[string]ProviderConfig{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(b), c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if c.Provider == nil {
		c.Provider = map[string]ProviderConfig{}
	}
	return c, nil
}

// Save writes c back atomically (tmpfile + rename). Permissions are
// 0600 — config can contain non-public defaults like model IDs.
func Save(c *Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	return saveTo(p, c)
}

func saveTo(path string, c *Config) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.toml.*")
	if err != nil {
		return fmt.Errorf("config: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: chmod %s: %w", tmpPath, err)
	}
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(c); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("config: rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// ProviderOf returns the merged settings for a provider, never nil.
// Missing entries return a zero ProviderConfig.
func (c *Config) ProviderOf(name string) ProviderConfig {
	if c == nil || c.Provider == nil {
		return ProviderConfig{}
	}
	return c.Provider[name]
}
