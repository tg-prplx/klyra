package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"klyra/pkg/llm"
	"klyra/pkg/policy"
)

type Invocation struct {
	CWD          string
	Sandbox      string
	Mode         string
	ContextFiles []string
	Args         map[string]any
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
		FileOutline{},
		SymbolReader{},
		GoSymbolReader{},
		WebSearch{},
		FetchURL{},
		FileWriter{},
		FileCreator{},
		ReplaceLines{},
		InsertLines{},
		ReplaceSymbol{},
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
	return r.SpecsForTaskMode(task, "", nil)
}

func (r *Registry) SpecsForTaskMode(task, mode string, contextFiles []string) []llm.ToolSpec {
	task = strings.ToLower(task)
	mode = strings.ToLower(strings.TrimSpace(mode))
	names := map[string]bool{
		"project_map":  true,
		"search":       true,
		"file_outline": true,
		"read_file":    true,
		"read_symbol":  true,
		"git_status":   true,
		"policy_check": true,
		"web_search":   true,
		"fetch_url":    true,
	}
	if mentionsGo(task) {
		names["read_go_symbol"] = true
	}
	if mentionsShell(task) || mentionsTest(task) {
		names["bash"] = true
	}
	writeIntent := mentionsEdit(task)
	skillCreateIntent := mentionsSkill(task) && writeIntent
	if writeIntent {
		names["diff_patch"] = true
		names["diff_preview"] = true
		names["insert_lines"] = true
		names["replace_lines"] = true
		names["replace_symbol"] = true
		names["create_file"] = true
		names["read_go_symbol"] = true
		names["bash"] = true
		names["git_diff"] = true
		names["workspace_checkpoint"] = true
	}
	switch mode {
	case "inspect":
		delete(names, "bash")
		delete(names, "diff_patch")
		delete(names, "diff_preview")
		delete(names, "create_file")
		delete(names, "git_diff")
		delete(names, "insert_lines")
		delete(names, "replace_lines")
		delete(names, "replace_symbol")
		delete(names, "write_file")
		delete(names, "workspace_checkpoint")
		delete(names, "workspace_restore")
	case "repair":
		names["git_diff"] = true
		names["bash"] = true
		delete(names, "workspace_restore")
	case "refactor":
		names["search"] = true
		names["git_diff"] = true
		names["diff_preview"] = true
		names["bash"] = true
		if len(contextFiles) > 0 {
			names["diff_patch"] = true
			names["insert_lines"] = true
			names["replace_lines"] = true
			names["replace_symbol"] = true
			names["create_file"] = true
			names["workspace_checkpoint"] = true
		}
	case "edit":
		if len(contextFiles) == 0 {
			if !skillCreateIntent {
				delete(names, "create_file")
			}
			delete(names, "diff_patch")
			delete(names, "insert_lines")
			delete(names, "replace_lines")
			delete(names, "replace_symbol")
			delete(names, "write_file")
		}
	}
	if len(task) < 80 && !mentionsEdit(task) && !mentionsShell(task) {
		names["list_files"] = true
	}

	specs := make([]llm.ToolSpec, 0, len(names))
	for name := range r.tools {
		if isMCPTool(name) {
			names[name] = true
		}
	}
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
	return r.RunWithPolicy(ctx, cwd, sandbox, "", nil, call)
}

func (r *Registry) RunWithPolicy(ctx context.Context, cwd string, sandbox string, mode string, contextFiles []string, call llm.ToolCall) (Result, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if err := enforceMode(mode, contextFiles, call); err != nil {
		return Result{Output: err.Error()}, err
	}
	if err := enforceSandbox(sandbox, call); err != nil {
		return Result{Output: err.Error()}, err
	}
	if err := enforceWriteToolUsage(cwd, mode, call); err != nil {
		return Result{Output: err.Error()}, err
	}
	return tool.Run(ctx, Invocation{CWD: cwd, Sandbox: sandbox, Mode: mode, ContextFiles: contextFiles, Args: call.Arguments})
}

func enforceMode(mode string, contextFiles []string, call llm.ToolCall) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "inspect":
		if isWriteTool(call.Name) {
			return fmt.Errorf("mode inspect blocks %s", call.Name)
		}
	case "edit":
		if isFileWriteTool(call.Name) {
			if call.Name == "create_file" && isProjectSkillPath(primaryWritePath(call)) {
				return nil
			}
			if len(contextFiles) == 0 {
				return fmt.Errorf("mode edit requires files in context cart before %s", call.Name)
			}
			if path := primaryWritePath(call); path != "" && !pathAllowed(path, contextFiles) {
				return fmt.Errorf("mode edit blocks %s outside context cart: %s", call.Name, path)
			}
		}
	case "refactor":
		if call.Name == "diff_patch" && len(contextFiles) == 0 {
			return fmt.Errorf("mode refactor requires context cart and dry-run evidence before diff_patch")
		}
	}
	return nil
}

func isWriteTool(name string) bool {
	return name == "write_file" || name == "create_file" || name == "diff_patch" || name == "replace_lines" || name == "insert_lines" || name == "replace_symbol" || name == "workspace_restore" || name == "bash"
}

func isFileWriteTool(name string) bool {
	return name == "write_file" || name == "create_file" || name == "diff_patch" || name == "replace_lines" || name == "insert_lines" || name == "replace_symbol"
}

func primaryWritePath(call llm.ToolCall) string {
	if call.Name == "write_file" || call.Name == "create_file" || call.Name == "replace_lines" || call.Name == "insert_lines" || call.Name == "replace_symbol" {
		path, _ := call.Arguments["path"].(string)
		return path
	}
	if call.Name == "diff_patch" {
		patch, _ := call.Arguments["patch"].(string)
		return firstPatchPath(patch)
	}
	return ""
}

func firstPatchPath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimPrefix(strings.TrimSpace(line), "+++ b/")
		}
	}
	return ""
}

func pathAllowed(path string, contextFiles []string) bool {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "./")
	for _, allowed := range contextFiles {
		allowed = strings.Trim(strings.ReplaceAll(allowed, "\\", "/"), "./")
		if path == allowed {
			return true
		}
	}
	return false
}

func isProjectSkillPath(path string) bool {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	if path == "" || strings.Contains(path, "../") {
		return false
	}
	if strings.HasPrefix(path, ".klyra/skills/") || strings.HasPrefix(path, ".agentcli/skills/") || strings.HasPrefix(path, "skills/") {
		return strings.HasSuffix(path, ".md")
	}
	return false
}

func enforceWriteToolUsage(cwd, mode string, call llm.ToolCall) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if call.Name != "write_file" || (mode != "edit" && mode != "refactor" && mode != "repair") {
		return nil
	}
	path, _ := call.Arguments["path"].(string)
	if strings.TrimSpace(path) == "" {
		return nil
	}
	target, err := safeWorkspacePath(cwd, path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("write_file refuses to overwrite existing file %s in %s mode; use replace_symbol, replace_lines, insert_lines, or diff_patch", path, mode)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func enforceSandbox(sandbox string, call llm.ToolCall) error {
	profile := policy.NormalizeSandbox(sandbox)
	if isMCPTool(call.Name) && profile == policy.SandboxReadOnly {
		return fmt.Errorf("sandbox %s blocks external MCP tool %s", profile, call.Name)
	}
	switch call.Name {
	case "write_file", "create_file", "diff_patch", "replace_lines", "insert_lines", "replace_symbol", "workspace_restore":
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

func TaskLooksLikeWriteRequest(task string) bool {
	return mentionsEdit(strings.ToLower(task))
}

func mentionsSkill(task string) bool {
	return containsAny(task, []string{"skill", "skills", "скилл", "скил", "навык"})
}

func mentionsWeb(task string) bool {
	return containsAny(task, []string{
		"http://", "https://", "web", "internet", "online", "site", "url", "latest", "current", "today", "news",
		"интернет", "в интернете", "веб", "сайт", "ссылк", "url", "актуаль", "последн", "новост", "сегодня", "найди в сети",
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
	if isMCPTool(name) {
		return true
	}
	switch name {
	case "bash", "write_file", "create_file", "diff_patch", "replace_lines", "insert_lines", "replace_symbol", "workspace_restore":
		return true
	default:
		return false
	}
}

func isMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp_")
}
