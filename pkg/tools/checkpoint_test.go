package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCheckpointAndRestore(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := WorkspaceCheckpoint{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"id": "before"},
	})
	if err != nil {
		t.Fatalf("checkpoint failed: %v (%s)", err, result.Output)
	}
	if err := os.WriteFile(file, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = WorkspaceRestore{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"id": "before"},
	})
	if err != nil {
		t.Fatalf("restore failed: %v (%s)", err, result.Output)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before\n" {
		t.Fatalf("restore did not restore file: %q", data)
	}
}

func TestWorkspaceCheckpointList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (WorkspaceCheckpoint{}).Run(context.Background(), Invocation{CWD: dir, Args: map[string]any{"id": "one"}}); err != nil {
		t.Fatal(err)
	}
	result, err := WorkspaceCheckpointList{}.Run(context.Background(), Invocation{CWD: dir, Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "one") {
		t.Fatalf("expected checkpoint in list: %s", result.Output)
	}
}
