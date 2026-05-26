package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectMapReturnsCompactImportantFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module sample\n")
	writeTestFile(t, dir, "cmd/app/main.go", "package main\n")
	writeTestFile(t, dir, "node_modules/ignored/index.js", "ignored\n")

	result, err := ProjectMap{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"max_files": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "go.mod") || !strings.Contains(result.Output, "cmd/app/main.go") {
		t.Fatalf("expected important files in project map:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "node_modules") {
		t.Fatalf("expected generated directories to be skipped:\n%s", result.Output)
	}
}

func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
