package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadScopedMatchesRulesByContextPath(t *testing.T) {
	root := t.TempDir()
	writeScopedFile(t, root, ".agentcli/recipes/migration-rules.md", "Use reversible migrations.")
	writeScopedFile(t, root, ".agentcli/recipes/frontend-style.md", "Keep components accessible.")

	got, err := LoadScoped(root, "change schema", []string{"db/migrations/202605270001_add_users.sql"}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 {
		t.Fatalf("expected one scoped rule, got %+v", got.Files)
	}
	if got.Files[0].Path != ".agentcli/recipes/migration-rules.md" || !strings.Contains(got.Content, "Use reversible migrations.") {
		t.Fatalf("unexpected scoped content: %+v\n%s", got.Files, got.Content)
	}
	if !strings.Contains(got.Files[0].Reason, "migration") {
		t.Fatalf("expected migration reason, got %+v", got.Files[0])
	}
}

func writeScopedFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
