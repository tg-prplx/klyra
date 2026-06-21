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
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(requested)
	targetInput := cleaned
	if !filepath.IsAbs(cleaned) {
		targetInput = filepath.Join(root, cleaned)
	}
	target, err := filepath.Abs(targetInput)
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
