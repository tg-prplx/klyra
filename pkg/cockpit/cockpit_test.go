package cockpit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCreatesBudgetedFactCards(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example\n")
	writeFile(t, root, "README.md", "# Example\n")
	writeFile(t, root, "pkg/app/app.go", "package app\n\nfunc Run() error { return nil }\n")
	writeFile(t, root, "AGENTS.md", "Keep changes small.\n")
	writeFile(t, root, ".agentcli/recipes/testing-rules.md", "Run focused tests.")
	writeFile(t, root, "go.sum", "example hash\n")

	snapshot, err := Build(context.Background(), Config{
		Enabled:         true,
		Inject:          true,
		MaxTokens:       600,
		MaxFiles:        10,
		IncludeDiff:     true,
		IncludeRecipes:  true,
		IncludeNegative: true,
	}, root, "test Run", []string{"pkg/app/app_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Enabled || !snapshot.Injected {
		t.Fatalf("expected enabled injected snapshot: %+v", snapshot)
	}
	if snapshot.EstimatedTokens > snapshot.MaxTokens {
		t.Fatalf("snapshot exceeded budget: %d > %d", snapshot.EstimatedTokens, snapshot.MaxTokens)
	}
	text := snapshot.Markdown()
	if strings.Contains(text, "###") || strings.Contains(text, "##") {
		t.Fatalf("cockpit markdown should avoid unsupported headers:\n%s", text)
	}
	for _, want := range []string{"Repo Map", "Context Cart", "Agent Rails", "Context Recipes", "Negative Context"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in cockpit markdown:\n%s", want, text)
		}
	}
}

func TestBuildDisabledReturnsDisabledSnapshot(t *testing.T) {
	snapshot, err := Build(context.Background(), Config{Enabled: false}, t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Enabled {
		t.Fatalf("expected disabled snapshot: %+v", snapshot)
	}
	if snapshot.Markdown() != "context cockpit disabled" {
		t.Fatalf("unexpected markdown: %q", snapshot.Markdown())
	}
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
