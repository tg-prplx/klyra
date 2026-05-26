package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func safeWorkspacePath(cwd, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", requested)
	}

	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.Clean(requested)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %s", requested)
	}
	return target, nil
}
