package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Provider       string             `json:"provider"`
	Model          string             `json:"model"`
	ModelRoutes    map[string]string  `json:"model_routes,omitempty"`
	Reasoning      string             `json:"reasoning,omitempty"`
	MaxSteps       int                `json:"max_steps"`
	MaxMessages    int                `json:"max_messages"`
	MaxContext     int                `json:"max_context_tokens"`
	MaxOutput      int                `json:"max_output_tokens"`
	ApprovalMode   string             `json:"approval_mode"`
	Sandbox        string             `json:"sandbox"`
	StoreResponses bool               `json:"store_responses"`
	Profiles       map[string]Profile `json:"profiles,omitempty"`
}

type Profile struct {
	Provider       string            `json:"provider,omitempty"`
	Model          string            `json:"model,omitempty"`
	ModelRoutes    map[string]string `json:"model_routes,omitempty"`
	Reasoning      string            `json:"reasoning,omitempty"`
	MaxSteps       int               `json:"max_steps,omitempty"`
	MaxMessages    int               `json:"max_messages,omitempty"`
	MaxContext     int               `json:"max_context_tokens,omitempty"`
	MaxOutput      int               `json:"max_output_tokens,omitempty"`
	ApprovalMode   string            `json:"approval_mode,omitempty"`
	Sandbox        string            `json:"sandbox,omitempty"`
	StoreResponses *bool             `json:"store_responses,omitempty"`
}

func Default() Config {
	return Config{
		Provider:     "mock",
		Model:        "mock-agent",
		MaxSteps:     8,
		MaxMessages:  40,
		MaxContext:   24000,
		MaxOutput:    4096,
		ApprovalMode: "auto",
		Sandbox:      "workspace-write",
		Profiles: map[string]Profile{
			"local": {
				Provider: "mock",
				Model:    "mock-agent",
			},
			"ollama": {
				Provider:     "ollama",
				MaxSteps:     12,
				MaxContext:   32000,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"anthropic": {
				Provider:     "anthropic",
				MaxSteps:     12,
				MaxContext:   32000,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"coding": {
				Provider:     "openai",
				Reasoning:    "low",
				MaxSteps:     12,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"deep": {
				Provider:     "openai",
				Reasoning:    "medium",
				MaxSteps:     20,
				MaxContext:   64000,
				MaxOutput:    8192,
				ApprovalMode: "ask",
			},
		},
	}
}

func DefaultPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "agentcli", "config.json")
	}
	return filepath.Join(".", ".agentcli", "config.json")
}

func Load(path string) (Config, error) {
	cfg := Default()
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

func WriteDefault(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (c Config) WithProfile(name string) (Config, error) {
	c.applyDefaults()
	name = strings.TrimSpace(name)
	if name == "" {
		return c, nil
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return Config{}, fmt.Errorf("profile %q not found", name)
	}
	providerChanged := profile.Provider != "" && profile.Provider != c.Provider
	if profile.Provider != "" {
		c.Provider = profile.Provider
	}
	if profile.Model != "" {
		c.Model = profile.Model
	} else if providerChanged {
		c.Model = ""
	}
	if profile.ModelRoutes != nil {
		c.ModelRoutes = profile.ModelRoutes
	}
	if profile.Reasoning != "" {
		c.Reasoning = profile.Reasoning
	}
	if profile.MaxSteps > 0 {
		c.MaxSteps = profile.MaxSteps
	}
	if profile.MaxMessages > 0 {
		c.MaxMessages = profile.MaxMessages
	}
	if profile.MaxContext > 0 {
		c.MaxContext = profile.MaxContext
	}
	if profile.MaxOutput > 0 {
		c.MaxOutput = profile.MaxOutput
	}
	if profile.ApprovalMode != "" {
		c.ApprovalMode = profile.ApprovalMode
	}
	if profile.Sandbox != "" {
		c.Sandbox = profile.Sandbox
	}
	if profile.StoreResponses != nil {
		c.StoreResponses = *profile.StoreResponses
	}
	return c, nil
}

func (c *Config) applyDefaults() {
	defaults := Default()
	if c.Provider == "" {
		c.Provider = defaults.Provider
	}
	if c.Model == "" && c.Provider == "mock" {
		c.Model = defaults.Model
	}
	if c.MaxSteps <= 0 {
		c.MaxSteps = defaults.MaxSteps
	}
	if c.MaxMessages <= 0 {
		c.MaxMessages = defaults.MaxMessages
	}
	if c.MaxContext <= 0 {
		c.MaxContext = defaults.MaxContext
	}
	if c.MaxOutput <= 0 {
		c.MaxOutput = defaults.MaxOutput
	}
	if c.ApprovalMode == "" {
		c.ApprovalMode = defaults.ApprovalMode
	}
	if c.Sandbox == "" {
		c.Sandbox = defaults.Sandbox
	}
	if c.Profiles == nil {
		c.Profiles = defaults.Profiles
	}
}
