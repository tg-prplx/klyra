package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentcli/pkg/llm"
)

type WorkspaceCheckpoint struct{}

func (WorkspaceCheckpoint) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "workspace_checkpoint",
		Description: "Create a workspace checkpoint under .agentcli/checkpoints before risky edits.",
		Parameters: objectSchema(map[string]any{
			"id": stringProperty("Optional checkpoint id."),
		}),
	}
}

func (WorkspaceCheckpoint) Run(_ context.Context, inv Invocation) (Result, error) {
	id, err := optionalStringArg(inv.Args, "id", "")
	if err != nil {
		return Result{}, err
	}
	id = cleanCheckpointID(id)
	if id == "" {
		id = time.Now().UTC().Format("20060102-150405")
	}
	target := filepath.Join(inv.CWD, ".agentcli", "checkpoints", id)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Result{}, err
	}
	files, err := copyWorkspace(inv.CWD, target)
	if err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("checkpoint %s created with %d files", id, files)}, nil
}

type WorkspaceRestore struct{}

func (WorkspaceRestore) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "workspace_restore",
		Description: "Restore files from a workspace checkpoint. This overwrites matching files but does not delete newer files.",
		Parameters: objectSchema(map[string]any{
			"id": stringProperty("Checkpoint id."),
		}, "id"),
	}
}

func (WorkspaceRestore) Run(_ context.Context, inv Invocation) (Result, error) {
	id, err := stringArg(inv.Args, "id")
	if err != nil {
		return Result{}, err
	}
	id = cleanCheckpointID(id)
	if id == "" {
		return Result{}, fmt.Errorf("checkpoint id cannot be empty")
	}
	source := filepath.Join(inv.CWD, ".agentcli", "checkpoints", id)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("checkpoint %q not found", id)
	}
	files, err := copyWorkspace(source, inv.CWD)
	if err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("checkpoint %s restored with %d files", id, files)}, nil
}

type WorkspaceCheckpointList struct{}

func (WorkspaceCheckpointList) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "workspace_checkpoints",
		Description: "List workspace checkpoints.",
		Parameters:  objectSchema(map[string]any{}),
	}
}

func (WorkspaceCheckpointList) Run(_ context.Context, inv Invocation) (Result, error) {
	root := filepath.Join(inv.CWD, ".agentcli", "checkpoints")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Output: "no checkpoints"}, nil
		}
		return Result{}, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return Result{Output: "no checkpoints"}, nil
	}
	return Result{Output: strings.Join(names, "\n")}, nil
}

func copyWorkspace(sourceRoot, targetRoot string) (int, error) {
	count := 0
	err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRoot {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(targetRoot, rel), 0o755)
		}
		if shouldSkipFile(entry.Name()) {
			return nil
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		info, err := entry.Info()
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetRoot, rel)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer target.Close()
		if _, err := io.Copy(target, source); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func cleanCheckpointID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, id)
	return strings.Trim(id, ".-")
}
