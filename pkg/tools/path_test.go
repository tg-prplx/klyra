package tools

import "testing"

func TestSafeWorkspacePathRejectsEscapes(t *testing.T) {
	_, err := safeWorkspacePath(t.TempDir(), "../outside.txt")
	if err == nil {
		t.Fatal("expected workspace escape to be rejected")
	}
}

func TestSafeWorkspacePathRejectsAbsolute(t *testing.T) {
	_, err := safeWorkspacePath(t.TempDir(), "/tmp/outside.txt")
	if err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}
