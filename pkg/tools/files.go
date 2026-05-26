package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agentcli/pkg/llm"
)

type ListFiles struct{}

func (ListFiles) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "list_files",
		Description: "List workspace files, skipping common generated directories.",
		Parameters: objectSchema(map[string]any{
			"max_files": integerProperty("Maximum number of files to return.", 1),
		}),
	}
}

func (ListFiles) Run(_ context.Context, inv Invocation) (Result, error) {
	maxFiles, err := optionalIntArg(inv.Args, "max_files", 200)
	if err != nil {
		return Result{}, err
	}

	var files []string
	err = filepath.WalkDir(inv.CWD, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && shouldSkipDir(entry.Name()) && path != inv.CWD {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if shouldSkipFile(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(inv.CWD, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	sort.Strings(files)
	if len(files) > maxFiles {
		files = append(files[:maxFiles], fmt.Sprintf("... %d more files", len(files)-maxFiles))
	}
	return Result{Output: strings.Join(files, "\n")}, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".agentcli", "node_modules", "dist", "build", ".cache", ".next", "vendor":
		return true
	default:
		return false
	}
}

func shouldSkipFile(name string) bool {
	switch name {
	case ".DS_Store":
		return true
	default:
		return false
	}
}

type FileReader struct{}

func (FileReader) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "read_file",
		Description: "Read a workspace file with optional line slicing.",
		Parameters: objectSchema(map[string]any{
			"path":       stringProperty("Relative file path."),
			"start_line": integerProperty("1-based start line.", 1),
			"max_lines":  integerProperty("Maximum lines to return.", 1),
		}, "path"),
	}
}

func (FileReader) Run(_ context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	startLine, err := optionalIntArg(inv.Args, "start_line", 1)
	if err != nil {
		return Result{}, err
	}
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 240)
	if err != nil {
		return Result{}, err
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return Result{}, err
	}
	lines := strings.Split(string(data), "\n")
	if startLine < 1 {
		startLine = 1
	}
	start := startLine - 1
	if start >= len(lines) {
		return Result{Output: ""}, nil
	}
	end := start + maxLines
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, fmt.Sprintf("%d: %s", i+1, lines[i]))
	}
	return Result{Output: strings.Join(out, "\n")}, nil
}

type FileWriter struct{}

func (FileWriter) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "write_file",
		Description: "Write a complete workspace file. Prefer diff_patch for edits to existing files.",
		Parameters: objectSchema(map[string]any{
			"path":    stringProperty("Relative file path."),
			"content": stringProperty("Complete file content."),
		}, "path", "content"),
	}
}

func (FileWriter) Run(_ context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	content, err := stringArg(inv.Args, "content")
	if err != nil {
		return Result{}, err
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("wrote %s (%d bytes)", requestedPath, len(content))}, nil
}
