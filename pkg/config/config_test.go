package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithProfileAppliesOverrides(t *testing.T) {
	cfg := Default()
	cfg.Model = "base-model"
	enabled := true
	cfg.Profiles["custom"] = Profile{
		Provider:       "openai",
		Model:          "custom-model",
		Reasoning:      "low",
		MaxSteps:       12,
		ApprovalMode:   "ask",
		StoreResponses: &enabled,
	}

	got, err := cfg.WithProfile("custom")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" || got.Model != "custom-model" || got.MaxSteps != 12 || got.ApprovalMode != "ask" || !got.StoreResponses {
		t.Fatalf("profile not applied: %+v", got)
	}
}

func TestWithProfileClearsInheritedModelWhenProviderChanges(t *testing.T) {
	cfg := Default()
	got, err := cfg.WithProfile("coding")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", got.Provider)
	}
	if got.Model != "" {
		t.Fatalf("expected model to be resolved from provider env, got %q", got.Model)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "mock" || cfg.MaxSteps == 0 || cfg.MaxContext == 0 {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	written, err := WriteDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if written != path {
		t.Fatalf("unexpected path: %s", written)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
