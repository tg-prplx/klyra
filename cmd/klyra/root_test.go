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
