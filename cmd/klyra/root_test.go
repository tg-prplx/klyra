package klyra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "klyra/pkg/config"
	"klyra/pkg/llm"
)

func TestTUILinesFromMessagesRestoresAssistantReasoning(t *testing.T) {
	lines := tuiLinesFromMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "done", Reasoning: "## Plan\n\n- inspect"},
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "thoughts:0:## Plan\n\n- inspect") {
		t.Fatalf("stored reasoning was not restored as thoughts: %#v", lines)
	}
	if !strings.Contains(joined, "agent: done") {
		t.Fatalf("assistant output missing: %#v", lines)
	}
}

func TestServiceMarkdownAvoidsUnsupportedHeaders(t *testing.T) {
	settings := formatTUISettings(appconfig.Default(), nil)
	cart := formatContextCart(nil)
	attachments := formatAttachments(nil)
	for name, text := range map[string]string{
		"settings":    settings,
		"cart":        cart,
		"attachments": attachments,
	} {
		if strings.Contains(text, "##") {
			t.Fatalf("%s output should not contain markdown headers:\n%s", name, text)
		}
	}
}

func TestTUILinesFromMessagesRestoresStats(t *testing.T) {
	lines := tuiLinesFromMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{
			Role:            llm.RoleAssistant,
			Content:         "done",
			DurationSeconds: 2.5,
			Usage: &llm.Usage{
				InputTokens:     1000,
				CachedTokens:    200,
				OutputTokens:    150,
				ReasoningTokens: 50,
				TotalTokens:     1150,
			},
		},
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "stats: duration=2.5s input=1000 cached=200 output=150 reasoning=50 total=1150") {
		t.Fatalf("stored stats were not restored properly: %#v", lines)
	}
}

func TestSkillsCommandShowsMatchedSkill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".klyra", "skills", "frontend.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("name: Frontend Cleanup\ntriggers: frontend, css\nAvoid glassmorphism."), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--cwd", dir, "skills", "--query", "frontend css", "--content"})
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Frontend Cleanup") || !strings.Contains(out.String(), "Avoid glassmorphism.") {
		t.Fatalf("skills output missing matched skill:\n%s", out.String())
	}
}

func TestTerminalTitleForProject(t *testing.T) {
	title := terminalTitleForProject(filepath.Join(t.TempDir(), "demo-project"))
	if title != "Klyra: demo-project" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestApplyTUISetStoresCustomProviderConfig(t *testing.T) {
	cfg := appconfig.Default()
	if err := applyTUISet(&cfg, []string{
		"provider=custom-openai",
		"endpoint=https://api.example.test/v1",
		"api_mode=responses",
	}); err != nil {
		t.Fatal(err)
	}
	custom, ok := cfg.CustomProviders["custom-openai"]
	if !ok {
		t.Fatalf("expected custom provider to be stored: %+v", cfg.CustomProviders)
	}
	if custom.BaseURL != "https://api.example.test/v1" {
		t.Fatalf("unexpected custom provider URL: %+v", custom)
	}
	if custom.APIType != "responses" {
		t.Fatalf("unexpected custom provider API type: %+v", custom)
	}
	if custom.APIKeyEnv != "KLYRA_PROVIDER_CUSTOM_OPENAI_API_KEY" {
		t.Fatalf("unexpected custom provider key env: %+v", custom)
	}
}

func TestBuildProviderFromConfigUsesCustomOpenAICompatibleProvider(t *testing.T) {
	const (
		keyEnv   = "KLYRA_PROVIDER_CUSTOM_OPENAI_API_KEY"
		modelEnv = "KLYRA_PROVIDER_CUSTOM_OPENAI_MODEL"
	)
	origKey := os.Getenv(keyEnv)
	origModel := os.Getenv(modelEnv)
	_ = os.Setenv(keyEnv, "test-key")
	_ = os.Setenv(modelEnv, "compat-model")
	defer func() {
		if origKey == "" {
			_ = os.Unsetenv(keyEnv)
		} else {
			_ = os.Setenv(keyEnv, origKey)
		}
		if origModel == "" {
			_ = os.Unsetenv(modelEnv)
		} else {
			_ = os.Setenv(modelEnv, origModel)
		}
	}()

	cfg := appconfig.Default()
	cfg.Provider = "custom-openai"
	cfg.Model = ""
	cfg.CustomProviders["custom-openai"] = appconfig.CustomProvider{
		BaseURL:   "https://api.example.test/v1",
		APIType:   "chat_completions",
		APIKeyEnv: keyEnv,
	}

	provider, model, err := buildProviderFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.(*llm.OpenAIProvider); !ok {
		t.Fatalf("expected OpenAI-compatible provider, got %T", provider)
	}
	if model != "compat-model" {
		t.Fatalf("expected model from custom provider env, got %q", model)
	}
}
