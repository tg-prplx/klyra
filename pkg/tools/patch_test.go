package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffPatcherAppliesUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "hello.txt")
	runGit(t, dir, "commit", "-m", "initial")

	patch := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"index ce01362..94954ab 100644",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello agent",
		"",
	}, "\n")

	result, err := DiffPatcher{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"patch": patch},
	})
	if err != nil {
		t.Fatalf("patch failed: %v (%s)", err, result.Output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello agent\n" {
		t.Fatalf("unexpected file content: %q", data)
	}
}

func TestDiffPreviewChecksPatchWithoutApplying(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "hello.txt")
	runGit(t, dir, "commit", "-m", "initial")

	patch := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"index ce01362..94954ab 100644",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello agent",
		"",
	}, "\n")

	result, err := DiffPreview{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"patch": patch},
	})
	if err != nil {
		t.Fatalf("preview failed: %v (%s)", err, result.Output)
	}
	if !strings.Contains(result.Output, "patch check passed") {
		t.Fatalf("unexpected preview output: %s", result.Output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("preview should not apply patch, got %q", data)
	}
}

func TestDiffPatcherAppliesUnifiedDiffWithoutGit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := strings.Join([]string{
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello without git",
		"",
	}, "\n")

	result, err := DiffPatcher{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"patch": patch},
	})
	if err != nil {
		t.Fatalf("patch failed: %v (%s)", err, result.Output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello without git\n" {
		t.Fatalf("unexpected file content: %q", data)
	}
}

func TestDiffPreviewChecksPatchWithoutApplyingWithoutGit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := strings.Join([]string{
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello preview",
		"",
	}, "\n")

	result, err := DiffPreview{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"patch": patch},
	})
	if err != nil {
		t.Fatalf("preview failed: %v (%s)", err, result.Output)
	}
	if !strings.Contains(result.Output, "hello.txt | +1 -1") {
		t.Fatalf("unexpected preview output: %s", result.Output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("preview should not apply patch, got %q", data)
	}
}

func TestDiffPatcherCreatesFileWithoutGit(t *testing.T) {
	dir := t.TempDir()
	patch := strings.Join([]string{
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1,2 @@",
		"+one",
		"+two",
		"",
	}, "\n")

	result, err := DiffPatcher{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"patch": patch},
	})
	if err != nil {
		t.Fatalf("patch failed: %v (%s)", err, result.Output)
	}
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\ntwo\n" {
		t.Fatalf("unexpected file content: %q", data)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
