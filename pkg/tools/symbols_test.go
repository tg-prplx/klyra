package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoSymbolReaderReadsSpecificMethod(t *testing.T) {
	dir := t.TempDir()
	source := `package sample

type Runner struct{}

func (r *Runner) Run() string {
	return "ok"
}

func Other() {}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GoSymbolReader{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":   "sample.go",
			"symbol": "Runner.Run",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Runner) Run") || strings.Contains(result.Output, "func Other") {
		t.Fatalf("unexpected symbol output:\n%s", result.Output)
	}
}
