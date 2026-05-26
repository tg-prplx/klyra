package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agentcli/pkg/llm"
	"agentcli/pkg/policy"
)

type Invocation struct {
	CWD     string
	Sandbox string
	Args    map[string]any
}

type Result struct {
	Output string
}

type Tool interface {
	Spec() llm.ToolSpec
	Run(ctx context.Context, inv Invocation) (Result, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(toolList ...Tool) *Registry {
	registry := &Registry{tools: make(map[string]Tool, len(toolList))}
	for _, tool := range toolList {
		registry.Register(tool)
	}
	return registry
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(
		ProjectMap{},
		GitStatus{},
		GitDiff{},
		WorkspaceCheckpoint{},
		WorkspaceCheckpointList{},
		WorkspaceRestore{},
		PolicyCheck{},
		BashRunner{},
		ListFiles{},
		FileReader{},
		GoSymbolReader{},
		FileWriter{},
		Search{},
		DiffPreview{},
		DiffPatcher{},
	)
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Spec().Name] = tool
}

func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, tool := range r.tools {
		specs = append(specs, tool.Spec())
	}
	sortSpecs(specs)
	return specs
}

func (r *Registry) SpecsForTask(task string) []llm.ToolSpec {
	task = strings.ToLower(task)
	names := map[string]bool{
		"project_map":  true,
		"search":       true,
		"read_file":    true,
		"git_status":   true,
		"policy_check": true,
	}
	if mentionsGo(task) {
		names["read_go_symbol"] = true
	}
	if mentionsShell(task) || mentionsTest(task) {
		names["bash"] = true
	}
	if mentionsEdit(task) {
		names["diff_patch"] = true
		names["diff_preview"] = true
		names["write_file"] = true
		names["read_go_symbol"] = true
		names["bash"] = true
		names["git_diff"] = true
		names["workspace_checkpoint"] = true
	}
	if len(task) < 80 && !mentionsEdit(task) && !mentionsShell(task) {
		names["list_files"] = true
	}

	specs := make([]llm.ToolSpec, 0, len(names))
	for name := range names {
		if tool, ok := r.tools[name]; ok {
			specs = append(specs, tool.Spec())
		}
	}
	sortSpecs(specs)
	return specs
}

func (r *Registry) Run(ctx context.Context, cwd string, call llm.ToolCall) (Result, error) {
	return r.RunWithSandbox(ctx, cwd, "", call)
}

func (r *Registry) RunWithSandbox(ctx context.Context, cwd string, sandbox string, call llm.ToolCall) (Result, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if err := enforceSandbox(sandbox, call); err != nil {
		return Result{Output: err.Error()}, err
	}
	return tool.Run(ctx, Invocation{CWD: cwd, Sandbox: sandbox, Args: call.Arguments})
}

func enforceSandbox(sandbox string, call llm.ToolCall) error {
	profile := policy.NormalizeSandbox(sandbox)
	switch call.Name {
	case "write_file", "diff_patch", "workspace_restore":
		if profile == policy.SandboxReadOnly {
			return fmt.Errorf("sandbox %s blocks %s", profile, call.Name)
		}
	case "bash":
		command, _ := call.Arguments["command"].(string)
		assessment := policy.AssessShellCommand(command)
		if ok, reason := policy.IsAllowedInSandbox(assessment, profile); !ok {
			return fmt.Errorf("sandbox %s blocks bash command: %s", profile, reason)
		}
	}
	return nil
}

func sortSpecs(specs []llm.ToolSpec) {
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
}

func mentionsGo(task string) bool {
	return strings.Contains(task, ".go") || strings.Contains(task, " go ") || strings.Contains(task, "golang")
}

func mentionsShell(task string) bool {
	return containsAny(task, []string{"run ", "запусти", "команд", "bash", "shell", "terminal", "build", "сбор", "lint", "test", "тест"})
}

func mentionsTest(task string) bool {
	return containsAny(task, []string{"test", "тест", "verify", "проверь", "validation", "smoke"})
}

func mentionsEdit(task string) bool {
	return containsAny(task, []string{
		"implement", "add ", "fix", "change", "edit", "write", "refactor", "delete",
		"реализ", "добав", "исправ", "измени", "поправ", "напиши", "удали", "рефактор",
	})
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func RequiresApproval(name string) bool {
	switch name {
	case "bash", "write_file", "diff_patch", "workspace_restore":
		return true
	default:
		return false
	}
}
