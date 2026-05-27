package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMatchesSkillByTrigger(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, ".klyra/skills/frontend.md", `---
name: Frontend Style
description: React and CSS design rules
triggers: frontend, style, css
---
Use accessible spacing and avoid glassmorphism.
`)

	result, err := Load(root, "remove glass from frontend css", nil, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 1 {
		t.Fatalf("expected one matched skill: %+v", result)
	}
	if result.Skills[0].Name != "Frontend Style" || !strings.Contains(result.Content, "avoid glassmorphism") {
		t.Fatalf("unexpected skill content: %+v", result)
	}
}

func TestLoadMatchesSkillByContextPath(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, ".agentcli/skills/migrations/SKILL.md", `name: Migration Safety
triggers: migration, database
Use reversible migrations.
`)

	result, err := Load(root, "change schema", []string{"db/migrations/001_add_users.sql"}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Reason == "" {
		t.Fatalf("expected migration skill match: %+v", result)
	}
}

func TestLoadAllUsesBudget(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "skills/large.md", "name: Large\ntriggers: large\n"+strings.Repeat("x", 200))

	result, err := LoadAll(root, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Skills) != 1 || !result.Skills[0].Truncated {
		t.Fatalf("expected truncated skill: %+v", result)
	}
}

func writeSkill(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
