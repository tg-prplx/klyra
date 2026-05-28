package tools

import (
	"context"
	"testing"

	"klyra/pkg/llm"
)

func TestSpecsForTaskPrunesWriteToolsForInspection(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("inspect project")
	if hasSpec(specs, "write_file") || hasSpec(specs, "diff_patch") || hasSpec(specs, "replace_symbol") {
		t.Fatalf("inspection should not include edit tools: %+v", specs)
	}
	if !hasSpec(specs, "project_map") || !hasSpec(specs, "file_outline") || !hasSpec(specs, "read_symbol") || !hasSpec(specs, "read_file") {
		t.Fatalf("inspection should include retrieval tools: %+v", specs)
	}
}

func TestSpecsForTaskIncludesEditToolsForImplementation(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("реализуй поддержку go tests")
	if hasSpec(specs, "write_file") {
		t.Fatalf("implementation should hide primitive full-file writer: %+v", specs)
	}
	if !hasSpec(specs, "create_file") || !hasSpec(specs, "diff_patch") || !hasSpec(specs, "replace_symbol") || !hasSpec(specs, "replace_lines") || !hasSpec(specs, "insert_lines") || !hasSpec(specs, "bash") || !hasSpec(specs, "workspace_checkpoint") {
		t.Fatalf("implementation should include edit and verification tools: %+v", specs)
	}
}

func TestEditModeExposesCreateFileForSkillCreationWithoutContextCart(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("напиши сам себе скилл для issue summary", "edit", nil)
	if !hasSpec(specs, "create_file") {
		t.Fatalf("skill creation should expose create_file even without context cart: %+v", specs)
	}
	if hasSpec(specs, "diff_patch") || hasSpec(specs, "replace_lines") || hasSpec(specs, "insert_lines") {
		t.Fatalf("skill creation without context cart should not expose broad edit tools: %+v", specs)
	}
}

func TestRunWithSandboxBlocksWriteInReadOnly(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithSandbox(context.Background(), t.TempDir(), "read-only", llm.ToolCall{
		Name:      "write_file",
		Arguments: map[string]any{"path": "x.txt", "content": "x"},
	})
	if err == nil {
		t.Fatal("expected read-only sandbox to block write_file")
	}
}

func TestRunWithSandboxBlocksNetworkInWorkspaceWrite(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithSandbox(context.Background(), t.TempDir(), "workspace-write", llm.ToolCall{
		Name:      "bash",
		Arguments: map[string]any{"command": "git fetch origin"},
	})
	if err == nil {
		t.Fatal("expected workspace-write sandbox to block network command")
	}
}

func TestSpecsForInspectModeBlocksEditTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("fix bug", "inspect", nil)
	if hasSpec(specs, "write_file") || hasSpec(specs, "diff_patch") || hasSpec(specs, "replace_symbol") || hasSpec(specs, "bash") {
		t.Fatalf("inspect mode should expose retrieval only: %+v", specs)
	}
}

func TestEditModeRequiresContextCart(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithPolicy(context.Background(), t.TempDir(), "workspace-write", "edit", nil, llm.ToolCall{
		Name:      "write_file",
		Arguments: map[string]any{"path": "main.go", "content": "x"},
	})
	if err == nil {
		t.Fatal("expected edit mode to require context cart")
	}
}

func TestEditModeBlocksFilesOutsideContextCart(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithPolicy(context.Background(), t.TempDir(), "workspace-write", "edit", []string{"allowed.go"}, llm.ToolCall{
		Name:      "write_file",
		Arguments: map[string]any{"path": "other.go", "content": "x"},
	})
	if err == nil {
		t.Fatal("expected edit mode to block files outside context cart")
	}
}

func TestEditModeBlocksCreateFileOutsideContextCart(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithPolicy(context.Background(), t.TempDir(), "workspace-write", "edit", []string{"allowed.go"}, llm.ToolCall{
		Name:      "create_file",
		Arguments: map[string]any{"path": "other.go", "content": "x"},
	})
	if err == nil {
		t.Fatal("expected edit mode to block create_file outside context cart")
	}
}

func TestEditModeAllowsProjectSkillCreateWithoutContextCart(t *testing.T) {
	dir := t.TempDir()
	result, err := NewDefaultRegistry().RunWithPolicy(context.Background(), dir, "workspace-write", "edit", nil, llm.ToolCall{
		Name: "create_file",
		Arguments: map[string]any{
			"path":        ".klyra/skills/issues.md",
			"content":     "name: Issue Summary\ntriggers: issue, github\nSummarize linked issues.",
			"description": "skill for issue summaries",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output == "" {
		t.Fatal("expected create_file output")
	}
}

func TestEditModeBlocksSmallWriteToolsOutsideContextCart(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithPolicy(context.Background(), t.TempDir(), "workspace-write", "edit", []string{"allowed.go"}, llm.ToolCall{
		Name:      "replace_lines",
		Arguments: map[string]any{"path": "other.go", "start_line": 1, "end_line": 1, "content": "x"},
	})
	if err == nil {
		t.Fatal("expected edit mode to block replace_lines outside context cart")
	}
}

func TestEditModeBlocksWriteFileOverExistingFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "allowed.go", "package main\n")
	_, err := NewDefaultRegistry().RunWithPolicy(context.Background(), dir, "workspace-write", "edit", []string{"allowed.go"}, llm.ToolCall{
		Name:      "write_file",
		Arguments: map[string]any{"path": "allowed.go", "content": "package main\n\nfunc main() {}\n"},
	})
	if err == nil {
		t.Fatal("expected edit mode to block primitive write_file overwrite")
	}
}

func TestCreateFileRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "new.go", "package main\n")
	_, err := NewDefaultRegistry().RunWithPolicy(context.Background(), dir, "workspace-write", "edit", []string{"new.go"}, llm.ToolCall{
		Name:      "create_file",
		Arguments: map[string]any{"path": "new.go", "content": "package main\n"},
	})
	if err == nil {
		t.Fatal("expected create_file to refuse overwrite")
	}
}

func TestReadOnlySandboxBlocksSmallWriteTools(t *testing.T) {
	_, err := NewDefaultRegistry().RunWithSandbox(context.Background(), t.TempDir(), "read-only", llm.ToolCall{
		Name:      "insert_lines",
		Arguments: map[string]any{"path": "x.txt", "after_line": 0, "content": "x"},
	})
	if err == nil {
		t.Fatal("expected read-only sandbox to block insert_lines")
	}
}

func hasSpec(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}
