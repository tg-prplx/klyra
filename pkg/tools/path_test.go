package tools

import "testing"

func TestSafeWorkspacePathRejectsEscapes(t *testing.T) {
	_, err := safeWorkspacePath(t.TempDir(), "../outside.txt")
	if err == nil {
		t.Fatal("expected workspace escape to be rejected")
	}
}

func TestSafeWorkspacePathRejectsAbsoluteOutsideWorkspace(t *testing.T) {
	_, err := safeWorkspacePath(t.TempDir(), "/tmp/outside.txt")
	if err == nil {
		t.Fatal("expected absolute path outside workspace to be rejected")
	}
}

func TestSafeWorkspacePathAcceptsAbsoluteInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	target, err := safeWorkspacePath(root, root+"/nested/file.txt")
	if err != nil {
		t.Fatalf("expected workspace absolute path to be accepted, got %v", err)
	}
	if target == "" {
		t.Fatal("expected normalized target path")
	}
}
