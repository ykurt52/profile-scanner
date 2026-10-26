// Package config manages the settings file stored next to the exe (portable mode).
package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
)

const name = "skillscanner.config.json"

// Config holds user settings. Secrets are written to disk encrypted with DPAPI.
type Config struct {
	Token string `json:"-"` // GitHub token
	Key   string `json:"-"` // Anthropic API key

	TokenEnc string `json:"github_token_enc,omitempty"`
	KeyEnc   string `json:"anthropic_key_enc,omitempty"`

	Model   string `json:"model"`
	Repos   int    `json:"max_repos"`       // max repos to analyze
	Budget  int    `json:"request_budget"`  // max API requests per run
	Reserve int    `json:"reserve_percent"` // share of quota that is never spent
	Delay   int    `json:"delay_ms"`        // pause between requests
}

func Defaults() Config {
	return Config{
		Model:   "claude-opus-5-5",
		Repos:   30,
		Budget:  300,
		Reserve: 20,
		Delay:   250,
	}
}

// Dir returns the folder of the exe. All data (config, cache, reports) lives there.
func Dir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func file() string { return filepath.Join(Dir(), name) }

func Load() Config {
	c := Defaults()
	b, err := os.ReadFile(file())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	c.Token = decrypt(c.TokenEnc)
	c.Key = decrypt(c.KeyEnc)
	c.fix()
	return c
}

func (c *Config) fix() {
	d := Defaults()
	if c.Model == "" {
		c.Model = d.Model
	}
	if c.Repos <= 0 {
		c.Repos = d.Repos
	}
	if c.Budget <= 0 {
		c.Budget = d.Budget
	}
	if c.Reserve < 5 || c.Reserve > 90 {
		c.Reserve = d.Reserve
	}
	if c.Delay < 100 {
		c.Delay = 100 // never go faster than this
	}
}

// Save writes the settings. Secrets are only written when keep is true.
func Save(c Config, keep bool) error {
	c.fix()
	c.TokenEnc, c.KeyEnc = "", ""
	if keep {
		c.TokenEnc = encrypt(c.Token)
		c.KeyEnc = encrypt(c.Key)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file(), b, 0o600)
}

func encrypt(s string) string {
	if s == "" {
		return ""
	}
	out, err := protect([]byte(s))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(out)
}

func decrypt(s string) string {
	if s == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	out, err := unprotect(raw)
	if err != nil {
		return "" // other machine or user: secret must be entered again
	}
	return string(out)
}
