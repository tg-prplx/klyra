package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileReaderLineSlice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FileReader{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":       "sample.txt",
			"start_line": 2,
			"max_lines":  1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) != "2: two" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
