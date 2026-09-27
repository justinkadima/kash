// Package config manages the persistent configuration for con.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all user-adjustable settings. It is stored as JSON.
type Config struct {
	// BaseURL is the OpenAI-compatible API endpoint root, e.g. an Ollama
	// server at http://127.0.0.1:11434. Requests hit {BaseURL}/v1/... .
	BaseURL string `json:"base_url"`
	// APIKey is an optional bearer token (ignored by Ollama, useful for
	// llama.cpp server, LM Studio, vLLM, OpenRouter, etc.).
	APIKey string `json:"api_key,omitempty"`
	// Model is the model id passed to the API, e.g. "llama3.1:8b".
	Model string `json:"model"`
	// Temperature is the sampling temperature.
	Temperature float64 `json:"temperature"`
	// ContextLines is how many lines of terminal scrollback are injected
	// into the system prompt with each chat message.
	ContextLines int `json:"context_lines"`
	// AutoRun executes proposed commands automatically when a response
	// completes. Off by default; the shell is unsandboxed.
	AutoRun bool `json:"auto_run"`
	// Shell overrides the shell binary; empty means $SHELL.
	Shell string `json:"shell,omitempty"`
	// ChatRatio is the fraction of the window width given to the chat pane.
	ChatRatio float64 `json:"chat_ratio"`
	// SystemPrompt is appended to the built-in system prompt.
	SystemPrompt string `json:"system_prompt,omitempty"`
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		BaseURL:      "http://127.0.0.1:11434",
		Model:        "",
		Temperature:  0.7,
		ContextLines: 60,
		AutoRun:      false,
		ChatRatio:    0.38,
	}
}

// Path resolves the config file path. An explicit override (flag or
// CON_CONFIG env) wins; otherwise the OS user config dir is used, which
// keeps the binary + its config fully relocatable via env var.
func Path(override string) (string, error) {
	if override == "" {
		override = os.Getenv("CON_CONFIG")
	}
	if override != "" {
		return filepath.Clean(override), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "con", "config.json"), nil
}

// Load reads the config at path, falling back to defaults for missing
// fields. A missing file is not an error.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	cfg.Normalize()
	return cfg, nil
}

// Save writes the config to path, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Normalize clamps values into sane ranges.
func (c *Config) Normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.BaseURL == "" {
		c.BaseURL = Default().BaseURL
	}
	if c.Temperature < 0 {
		c.Temperature = 0
	}
	if c.Temperature > 2 {
		c.Temperature = 2
	}
	if c.ContextLines < 0 {
		c.ContextLines = 0
	}
	if c.ContextLines > 500 {
		c.ContextLines = 500
	}
	if c.ChatRatio < 0.2 {
		c.ChatRatio = 0.2
	}
	if c.ChatRatio > 0.7 {
		c.ChatRatio = 0.7
	}
}
