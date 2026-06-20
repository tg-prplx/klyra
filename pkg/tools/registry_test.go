package tools

import (
	"context"
	"strings"
	"testing"

	"klyra/pkg/llm"
)

func TestTaskTextDoesNotGuessCapabilities(t *testing.T) {
	for _, task := range []string{"inspect project", "реализуй поддержку go tests", "найди twitch канал furrydev2007", "составь план рефакторинга"} {
		specs := NewDefaultRegistry().SpecsForTask(task)
		if len(specs) != 1 || !hasSpec(specs, "discover_tools") {
			t.Fatalf("task text should expose only capability discovery for %q: %+v", task, specs)
		}
	}
}

func TestToolSpecsAreMinimal(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForCapabilities("anything", "", nil, map[string]bool{CapabilityWorkspace: true, CapabilityEdit: true, CapabilityShell: true, CapabilityWeb: true})
	if len(specs) == 0 {
		t.Fatal("expected tool specs")
	}
	for _, spec := range specs {
		if strings.Contains(spec.Description, "Use this") || strings.Contains(spec.Description, "Prefer") || strings.Contains(spec.Description, "current task") {
			t.Fatalf("tool description should stay compact for %s: %q", spec.Name, spec.Description)
		}
		if schemaHasKey(spec.Parameters, "description") || schemaHasKey(spec.Parameters, "title") {
			t.Fatalf("tool schema should not include verbose metadata for %s: %+v", spec.Name, spec.Parameters)
		}
	}
}

func TestSpecsForExplicitFileEditExposeFocusedTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("исправь pkg/agent/agent.go", "edit", nil)
	if !hasSpec(specs, "read_file") || !hasSpec(specs, "replace_lines") || !hasSpec(specs, "replace_symbol") || !hasSpec(specs, "insert_lines") {
		t.Fatalf("explicit file edit should expose focused tools: %+v", specs)
	}
	if hasSpec(specs, "diff_patch") || hasSpec(specs, "bash") {
		t.Fatalf("explicit file edit should not expose patch/bash before needed: %+v", specs)
	}
}

func TestSpecsForWorkspaceEditModeExposeCreateFileWithoutFocusedEditors(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("python TUI diceplay проект", "edit", nil)
	if !hasSpec(specs, "create_file") {
		t.Fatalf("workspace task in edit mode should expose create_file immediately: %+v", specs)
	}
	for _, name := range []string{"replace_lines", "replace_symbol", "insert_lines", "diff_patch", "read_file"} {
		if hasSpec(specs, name) {
			t.Fatalf("new project without files should not expose %s: %+v", name, specs)
		}
	}
}

func TestSpecsWithContextCartExposePatchTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("исправь баг", "edit", []string{"pkg/agent/agent.go"})
	if !hasSpec(specs, "diff_patch") || !hasSpec(specs, "workspace_checkpoint") || !hasSpec(specs, "read_file") {
		t.Fatalf("context cart edit should expose heavier edit tools: %+v", specs)
	}
}

func TestSpecsForSimpleChatHidesWorkspaceTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("привет, как дела?")
	if len(specs) != 1 || !hasSpec(specs, "discover_tools") {
		t.Fatalf("simple chat should pay only for compact capability discovery: %+v", specs)
	}
}

func TestPlanModeExposesPlanningAndReadOnlyTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("спланируй исправление pkg/agent/agent.go", "plan", nil)
	for _, name := range []string{"update_plan", "project_map", "search", "file_outline", "read_file"} {
		if !hasSpec(specs, name) {
			t.Fatalf("plan mode should expose %s: %+v", name, specs)
		}
	}
	for _, name := range []string{"bash", "create_file", "replace_lines", "diff_patch"} {
		if hasSpec(specs, name) {
			t.Fatalf("plan mode should hide %s: %+v", name, specs)
		}
	}
}

func TestPlanModeBlocksDirectWritesAndExternalMCP(t *testing.T) {
	registry := NewDefaultRegistry()
	_, err := registry.RunWithPolicy(context.Background(), t.TempDir(), "workspace-write", "plan", nil, llm.ToolCall{
		Name:      "create_file",
		Arguments: map[string]any{"path": "blocked.txt", "content": "no"},
	})
	if err == nil || !strings.Contains(err.Error(), "mode plan blocks create_file") {
		t.Fatalf("expected plan mode write block, got %v", err)
	}

	registry.Register(MCPTool{
		server:   MCPServerConfig{Name: "demo"},
		toolName: "echo",
		spec:     llm.ToolSpec{Name: "mcp_demo_echo", Parameters: objectSchema(map[string]any{})},
	})
	if hasSpec(registry.SpecsForTaskMode("plan project changes", "plan", nil), "mcp_demo_echo") {
		t.Fatal("plan mode should hide external MCP tool schemas")
	}
	_, err = registry.RunWithPolicy(context.Background(), t.TempDir(), "workspace-write", "plan", nil, llm.ToolCall{Name: "mcp_demo_echo"})
	if err == nil || !strings.Contains(err.Error(), "mode plan blocks external MCP tool") {
		t.Fatalf("expected plan mode MCP block, got %v", err)
	}
}

func TestSpecsHideLegacyWriteFile(t *testing.T) {
	specs := NewDefaultRegistry().Specs()
	if hasSpec(specs, "write_file") {
		t.Fatalf("legacy write_file should not be exposed in tool schemas: %+v", specs)
	}
}

func TestSpecsForWebTaskUsesOnlyWebTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForCapabilities("arbitrary text", "", nil, map[string]bool{CapabilityWeb: true})
	if !hasSpec(specs, "web_search") || !hasSpec(specs, "fetch_url") || !hasSpec(specs, "guide") {
		t.Fatalf("web capability should expose web tools and guide: %+v", specs)
	}
	if hasSpec(specs, "project_map") || hasSpec(specs, "read_file") || hasSpec(specs, "git_status") || hasSpec(specs, "bash") {
		t.Fatalf("web task should not expose workspace tools: %+v", specs)
	}
}

func TestSpecsForCodeQuestionUsesWorkspaceTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("что в файле pkg/agent/agent.go?")
	if !hasSpec(specs, "project_map") || !hasSpec(specs, "read_file") || !hasSpec(specs, "file_outline") {
		t.Fatalf("code/file question should expose workspace tools: %+v", specs)
	}
}

func TestEditModeExposesCreateFileForSkillCreationWithoutContextCart(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("напиши сам себе скилл для issue summary", "edit", nil)
	if !hasSpec(specs, "create_file") {
		t.Fatalf("skill creation should expose create_file even without context cart: %+v", specs)
	}
	if hasSpec(specs, "guide") || hasSpec(specs, "project_map") || hasSpec(specs, "bash") || hasSpec(specs, "diff_patch") || hasSpec(specs, "replace_lines") || hasSpec(specs, "insert_lines") {
		t.Fatalf("skill creation without context cart should expose only focused tools: %+v", specs)
	}
}

func TestGuideReturnsSkillCreationWorkflow(t *testing.T) {
	result, err := Guide{}.Run(context.Background(), Invocation{
		CWD: t.TempDir(),
		Args: map[string]any{
			"query":    "напиши сам себе скилл для github issue summary",
			"workflow": "skill",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, ".klyra/skills") || !strings.Contains(result.Output, "Use create_file") || !strings.Contains(result.Output, "Do not inspect sessions, .env") {
		t.Fatalf("unexpected guide output:\n%s", result.Output)
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

func TestEditModeAllowsCreateFileWithoutContextCart(t *testing.T) {
	dir := t.TempDir()
	result, err := NewDefaultRegistry().RunWithPolicy(context.Background(), dir, "workspace-write", "edit", nil, llm.ToolCall{
		Name:      "create_file",
		Arguments: map[string]any{"path": "notes.md", "content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output == "" {
		t.Fatal("expected create_file output")
	}
}

func TestEditModeAllowsFocusedEditWithoutContextCart(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.md", "old\n")
	result, err := NewDefaultRegistry().RunWithPolicy(context.Background(), dir, "workspace-write", "edit", nil, llm.ToolCall{
		Name:      "replace_lines",
		Arguments: map[string]any{"path": "notes.md", "start_line": 1, "end_line": 1, "content": "new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output == "" {
		t.Fatal("expected replace_lines output")
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

func TestEditModeAllowsProjectSkillBundleSupportFileWithoutContextCart(t *testing.T) {
	dir := t.TempDir()
	result, err := NewDefaultRegistry().RunWithPolicy(context.Background(), dir, "workspace-write", "edit", nil, llm.ToolCall{
		Name: "create_file",
		Arguments: map[string]any{
			"path":        ".agentcli/skills/github-issues-summarizer/tools/fetch_issue.sh",
			"content":     "#!/bin/sh\n",
			"description": "support script for issue summary skill",
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

func schemaHasKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[key]; ok {
			return true
		}
		for _, nested := range typed {
			if schemaHasKey(nested, key) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if schemaHasKey(nested, key) {
				return true
			}
		}
	}
	return false
}
