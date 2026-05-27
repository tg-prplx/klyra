package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type unifiedPatchFile struct {
	OldPath string
	NewPath string
	Hunks   []unifiedPatchHunk
}

type unifiedPatchHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string
}

func previewUnifiedPatch(cwd, patch string) ([]string, error) {
	files, err := parseUnifiedPatch(patch)
	if err != nil {
		return nil, err
	}
	var stats []string
	for _, file := range files {
		if err := applyUnifiedPatchFile(cwd, file, true); err != nil {
			return nil, err
		}
		added, removed := fileStats(file)
		path := patchTargetPath(file)
		stats = append(stats, fmt.Sprintf("%s | +%d -%d", path, added, removed))
	}
	return stats, nil
}

func applyUnifiedPatch(cwd, patch string, checkOnly bool) error {
	files, err := parseUnifiedPatch(patch)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := applyUnifiedPatchFile(cwd, file, checkOnly); err != nil {
			return err
		}
	}
	return nil
}

func parseUnifiedPatch(patch string) ([]unifiedPatchFile, error) {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	var files []unifiedPatchFile
	var current *unifiedPatchFile
	var hunk *unifiedPatchHunk

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "--- "):
			if current != nil {
				if hunk != nil {
					current.Hunks = append(current.Hunks, *hunk)
					hunk = nil
				}
				files = append(files, *current)
			}
			oldPath := cleanPatchHeaderPath(strings.TrimSpace(strings.TrimPrefix(line, "--- ")))
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
				return nil, fmt.Errorf("missing +++ header after %s", line)
			}
			i++
			newPath := cleanPatchHeaderPath(strings.TrimSpace(strings.TrimPrefix(lines[i], "+++ ")))
			current = &unifiedPatchFile{OldPath: oldPath, NewPath: newPath}
		case strings.HasPrefix(line, "@@ "):
			if current == nil {
				return nil, fmt.Errorf("hunk before file header")
			}
			if hunk != nil {
				current.Hunks = append(current.Hunks, *hunk)
			}
			parsed, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			hunk = &parsed
		case hunk != nil:
			if line == `\ No newline at end of file` {
				continue
			}
			if line == "" {
				continue
			}
			prefix := line[0]
			if prefix != ' ' && prefix != '-' && prefix != '+' {
				return nil, fmt.Errorf("invalid hunk line prefix %q", prefix)
			}
			hunk.Lines = append(hunk.Lines, line)
		}
	}
	if current != nil {
		if hunk != nil {
			current.Hunks = append(current.Hunks, *hunk)
		}
		files = append(files, *current)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no unified diff file headers found")
	}
	for _, file := range files {
		if len(file.Hunks) == 0 {
			return nil, fmt.Errorf("patch for %s has no hunks", patchTargetPath(file))
		}
	}
	return files, nil
}

func parseHunkHeader(header string) (unifiedPatchHunk, error) {
	fields := strings.Fields(header)
	if len(fields) < 3 || fields[0] != "@@" {
		return unifiedPatchHunk{}, fmt.Errorf("invalid hunk header %q", header)
	}
	oldStart, oldCount, err := parseHunkRange(fields[1], '-')
	if err != nil {
		return unifiedPatchHunk{}, fmt.Errorf("invalid old range in %q: %w", header, err)
	}
	newStart, newCount, err := parseHunkRange(fields[2], '+')
	if err != nil {
		return unifiedPatchHunk{}, fmt.Errorf("invalid new range in %q: %w", header, err)
	}
	return unifiedPatchHunk{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount}, nil
}

func parseHunkRange(value string, prefix byte) (int, int, error) {
	if value == "" || value[0] != prefix {
		return 0, 0, fmt.Errorf("expected %c range", prefix)
	}
	value = value[1:]
	startText, countText, ok := strings.Cut(value, ",")
	start, err := strconv.Atoi(startText)
	if err != nil {
		return 0, 0, err
	}
	count := 1
	if ok {
		count, err = strconv.Atoi(countText)
		if err != nil {
			return 0, 0, err
		}
	}
	return start, count, nil
}

func applyUnifiedPatchFile(cwd string, file unifiedPatchFile, checkOnly bool) error {
	targetPath := patchTargetPath(file)
	target, err := safeWorkspacePath(cwd, targetPath)
	if err != nil {
		return err
	}

	var lines []string
	finalNewline := true
	if file.OldPath == "/dev/null" {
		lines = nil
	} else {
		lines, finalNewline, err = readEditableLines(target)
		if err != nil {
			return fmt.Errorf("%s: %w", targetPath, err)
		}
	}

	next, err := applyHunksToLines(targetPath, lines, file.Hunks)
	if err != nil {
		return err
	}
	if checkOnly {
		return nil
	}
	if file.NewPath == "/dev/null" {
		return os.Remove(target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return writeEditableLines(target, next, finalNewline)
}

func applyHunksToLines(path string, lines []string, hunks []unifiedPatchHunk) ([]string, error) {
	next := append([]string(nil), lines...)
	offset := 0
	for _, hunk := range hunks {
		start := hunk.OldStart
		if start <= 0 {
			start = 1
		}
		idx := start - 1 + offset
		if idx < 0 || idx > len(next) {
			return nil, fmt.Errorf("%s: hunk starts outside file at line %d", path, hunk.OldStart)
		}

		oldLines := make([]string, 0, hunk.OldCount)
		newLines := make([]string, 0, hunk.NewCount)
		for _, line := range hunk.Lines {
			body := line[1:]
			switch line[0] {
			case ' ':
				oldLines = append(oldLines, body)
				newLines = append(newLines, body)
			case '-':
				oldLines = append(oldLines, body)
			case '+':
				newLines = append(newLines, body)
			}
		}
		if idx+len(oldLines) > len(next) {
			return nil, fmt.Errorf("%s: hunk exceeds file length at line %d", path, hunk.OldStart)
		}
		for i, expected := range oldLines {
			if next[idx+i] != expected {
				return nil, fmt.Errorf("%s: hunk mismatch at line %d: expected %q, got %q", path, idx+i+1, expected, next[idx+i])
			}
		}
		replaced := make([]string, 0, len(next)-len(oldLines)+len(newLines))
		replaced = append(replaced, next[:idx]...)
		replaced = append(replaced, newLines...)
		replaced = append(replaced, next[idx+len(oldLines):]...)
		next = replaced
		offset += len(newLines) - len(oldLines)
	}
	return next, nil
}

func cleanPatchHeaderPath(path string) string {
	fields := strings.Fields(path)
	if len(fields) == 0 {
		return ""
	}
	path = fields[0]
	if path == "/dev/null" {
		return path
	}
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return filepath.ToSlash(path)
}

func patchTargetPath(file unifiedPatchFile) string {
	if file.NewPath != "" && file.NewPath != "/dev/null" {
		return file.NewPath
	}
	return file.OldPath
}

func fileStats(file unifiedPatchFile) (int, int) {
	var added, removed int
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line == "" {
				continue
			}
			switch line[0] {
			case '+':
				added++
			case '-':
				removed++
			}
		}
	}
	return added, removed
}
