package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitStatusReturnsCompactStatus(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := GitStatus{}.Run(context.Background(), Invocation{CWD: dir, Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "?? file.txt") {
		t.Fatalf("unexpected status:\n%s", result.Output)
	}
}
