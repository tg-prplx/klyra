package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"klyra/pkg/llm"
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

	notes := loadFileNotes(inv.CWD)
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
		rel = filepath.ToSlash(rel)
		if note := notes[rel]; note != "" {
			rel += "\t# " + note
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
	lower := strings.ToLower(name)
	switch name {
	case ".DS_Store", ".env":
		return true
	}
	return strings.HasPrefix(lower, ".env.") ||
		strings.HasSuffix(lower, ".pem") ||
		strings.HasSuffix(lower, ".key") ||
		strings.HasSuffix(lower, ".p12") ||
		strings.HasSuffix(lower, ".pfx")
}

type FileReader struct{}

func (FileReader) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "read_file",
		Description: "Read a file slice. Prefer file_outline/read_symbol first; keep slices near 100 lines.",
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
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 120)
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
		Description: "Disabled legacy full-file writer. Use create_file for new files and edit_file for existing files.",
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

type FileCreator struct{}

func (FileCreator) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "create_file",
		Description: "Write a complete file, replacing existing content if present. For focused changes, use edit_file.",
		Parameters: objectSchema(map[string]any{
			"path":        stringProperty("Relative file path."),
			"content":     stringProperty("Complete file content."),
			"description": stringProperty("Short internal note for this file, shown beside it in file lists."),
		}, "path", "content"),
	}
}

func (FileCreator) Run(_ context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	content, err := stringArg(inv.Args, "content")
	if err != nil {
		return Result{}, err
	}
	description, err := optionalStringArg(inv.Args, "description", "")
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
	output := fmt.Sprintf("wrote %s (%d bytes)", requestedPath, len(content))
	if note := cleanFileNote(description); note != "" {
		if err := saveFileNote(inv.CWD, requestedPath, note); err != nil {
			output += fmt.Sprintf("; description note failed: %v", err)
		} else {
			output += "; description: " + note
		}
	}
	return Result{Output: output}, nil
}

func loadFileNotes(cwd string) map[string]string {
	path := filepath.Join(cwd, ".agentcli", "file_notes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	var raw map[string]struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]string{}
	}
	notes := make(map[string]string, len(raw))
	for file, note := range raw {
		if cleaned := cleanFileNote(note.Description); cleaned != "" {
			notes[normalizeFileNotePath(file)] = cleaned
		}
	}
	return notes
}

func saveFileNote(cwd, requestedPath, description string) error {
	path := normalizeFileNotePath(requestedPath)
	if path == "" {
		return nil
	}
	root := filepath.Join(cwd, ".agentcli")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	notesPath := filepath.Join(root, "file_notes.json")
	notes := loadFileNotes(cwd)
	notes[path] = cleanFileNote(description)
	raw := make(map[string]map[string]string, len(notes))
	for file, note := range notes {
		raw[file] = map[string]string{"description": note}
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(notesPath, append(data, '\n'), 0o644)
}

func normalizeFileNotePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if path == "." {
		return ""
	}
	return strings.TrimPrefix(path, "./")
}

func cleanFileNote(description string) string {
	description = strings.Join(strings.Fields(description), " ")
	if len(description) > 180 {
		description = description[:180]
	}
	return description
}
