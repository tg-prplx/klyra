package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceLinesReplacesSmallRange(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n")

	result, err := ReplaceLines{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":       "main.go",
			"start_line": 4,
			"end_line":   4,
			"content":    "\tprintln(\"new\")",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "replaced main.go lines 4-4") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	content := readTestFile(t, dir, "main.go")
	if !strings.Contains(content, "println(\"new\")") || strings.Contains(content, "println(\"old\")") {
		t.Fatalf("unexpected file content:\n%s", content)
	}
}

func TestEditFileReplacesExactText(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n")
	result, err := EditFile{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path": "main.go",
			"old":  "println(\"old\")",
			"new":  "println(\"new\")",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "edited main.go") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `println("new")`) {
		t.Fatalf("file not edited:\n%s", data)
	}
}

func TestEditFileRequiresSpecificOldText(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "same\nsame\n")
	_, err := EditFile{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path": "main.go",
			"old":  "same",
			"new":  "next",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "matches 2 times") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestInsertLinesInsertsWithoutWholeFileRewritePrompt(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	_, err := InsertLines{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":       "main.go",
			"after_line": 1,
			"content":    "\nimport \"fmt\"",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := readTestFile(t, dir, "main.go")
	if !strings.Contains(content, "package main\n\nimport \"fmt\"\n\nfunc main") {
		t.Fatalf("unexpected file content:\n%s", content)
	}
}

func TestReplaceSymbolUsesTreeSitterRange(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "app.ts", `export function greet(name: string) {
  return "hi " + name;
}

export function keep() {
  return "same";
}
`)

	_, err := ReplaceSymbol{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":   "app.ts",
			"symbol": "greet",
			"content": `export function greet(name: string) {
  return "hello " + name;
}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := readTestFile(t, dir, "app.ts")
	if !strings.Contains(content, `"hello " + name`) || strings.Contains(content, `"hi " + name`) {
		t.Fatalf("symbol replacement did not update greet:\n%s", content)
	}
	if !strings.Contains(content, `export function keep()`) {
		t.Fatalf("symbol replacement touched other symbol:\n%s", content)
	}
}

func readTestFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
