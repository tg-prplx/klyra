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

func TestGitStatusOutsideRepositoryIsNonFatal(t *testing.T) {
	result, err := GitStatus{}.Run(context.Background(), Invocation{CWD: t.TempDir(), Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "not a git repository") {
		t.Fatalf("unexpected status:\n%s", result.Output)
	}
}

func TestGitDiffOutsideRepositoryIsNonFatal(t *testing.T) {
	result, err := GitDiff{}.Run(context.Background(), Invocation{CWD: t.TempDir(), Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "no tracked diff") {
		t.Fatalf("unexpected diff:\n%s", result.Output)
	}
}
