package tools

import (
	"context"
	"testing"

	"agentcli/pkg/llm"
)

func TestSpecsForTaskPrunesWriteToolsForInspection(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("inspect project")
	if hasSpec(specs, "write_file") || hasSpec(specs, "diff_patch") {
		t.Fatalf("inspection should not include edit tools: %+v", specs)
	}
	if !hasSpec(specs, "project_map") || !hasSpec(specs, "read_file") {
		t.Fatalf("inspection should include retrieval tools: %+v", specs)
	}
}

func TestSpecsForTaskIncludesEditToolsForImplementation(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("реализуй поддержку go tests")
	if !hasSpec(specs, "write_file") || !hasSpec(specs, "diff_patch") || !hasSpec(specs, "bash") || !hasSpec(specs, "workspace_checkpoint") {
		t.Fatalf("implementation should include edit and verification tools: %+v", specs)
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

func hasSpec(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}
