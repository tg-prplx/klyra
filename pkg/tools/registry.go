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
	tools    map[string]Tool
	disabled map[string]bool
}

func NewRegistry(toolList ...Tool) *Registry {
	registry := &Registry{
		tools:    make(map[string]Tool, len(toolList)),
		disabled: make(map[string]bool),
	}
	for _, tool := range toolList {
		registry.Register(tool)
	}
	return registry
}

func (r *Registry) SetDisabled(disabledList []string) {
	if r.disabled == nil {
		r.disabled = make(map[string]bool)
	} else {
		for k := range r.disabled {
			delete(r.disabled, k)
		}
	}
	for _, name := range disabledList {
		r.disabled[name] = true
	}
}

func (r *Registry) IsDisabled(name string) bool {
	if r.disabled == nil {
		return false
	}
	return r.disabled[name]
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(
		Guide{},
		UpdatePlan{},
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
	for name, tool := range r.tools {
		if isHiddenToolSpec(name) {
			continue
		}
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
	writeIntent := mentionsEdit(task)
	shellIntent := mentionsShell(task)
	testIntent := mentionsTest(task)
	webIntent := mentionsWeb(task)
	planningIntent := mentionsPlanning(task)
	explicitPathIntent := mentionsSpecificPath(task)
	codeIntent := len(contextFiles) > 0 || writeIntent || shellIntent || testIntent || mentionsCodeWorkspace(task)
	names := map[string]bool{}

	if codeIntent {
		names["guide"] = true
		names["project_map"] = true
		names["search"] = true
		names["file_outline"] = true
		names["read_symbol"] = true
		if len(contextFiles) > 0 || explicitPathIntent {
			names["read_file"] = true
		}
		if mentionsFileListing(task) {
			names["list_files"] = true
		}
		if writeIntent || testIntent || mentionsGit(task) {
			names["git_status"] = true
		}
		if shellIntent {
			names["policy_check"] = true
		}
	}
	if mode == "plan" || planningIntent || mode == "refactor" {
		names["update_plan"] = true
	}
	if webIntent {
		names["guide"] = true
		names["web_search"] = true
		names["fetch_url"] = true
	}
	if mentionsGo(task) || explicitPathIntent {
		names["read_go_symbol"] = true
	}
	if shellIntent || testIntent {
		names["bash"] = true
	}
	skillCreateIntent := mentionsSkill(task) && writeIntent
	focusedSkillCreation := skillCreateIntent && mode == "edit" && len(contextFiles) == 0
	if focusedSkillCreation {
		names = map[string]bool{
			"guide":       true,
			"create_file": true,
		}
	}
	if writeIntent && !focusedSkillCreation {
		if len(contextFiles) > 0 || explicitPathIntent || mentionsNewFile(task) {
			names["insert_lines"] = true
			names["replace_lines"] = true
			names["replace_symbol"] = true
			names["create_file"] = true
			names["read_file"] = true
			names["read_go_symbol"] = true
		}
		if len(contextFiles) > 0 {
			names["diff_patch"] = true
			names["diff_preview"] = true
			names["git_diff"] = true
			names["workspace_checkpoint"] = true
		}
	}
	switch mode {
	case "inspect", "plan":
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
			if !skillCreateIntent && !mentionsNewFile(task) && !explicitPathIntent {
				delete(names, "create_file")
				delete(names, "insert_lines")
				delete(names, "replace_lines")
				delete(names, "replace_symbol")
			}
			delete(names, "diff_patch")
			delete(names, "write_file")
		}
	}
	if codeIntent && len(task) < 80 && !writeIntent && !shellIntent && mentionsFileListing(task) {
		names["list_files"] = true
	}

	specs := make([]llm.ToolSpec, 0, len(names))
	for name := range r.tools {
		if isMCPTool(name) {
			if mode == "plan" {
				continue
			}
			names[name] = true
		}
	}
	for name := range names {
		if isHiddenToolSpec(name) {
			continue
		}
		if r.disabled != nil && r.disabled[name] {
			continue
		}
		if tool, ok := r.tools[name]; ok {
			specs = append(specs, tool.Spec())
		}
	}
	sortSpecs(specs)
	return specs
}

func isHiddenToolSpec(name string) bool {
	return name == "write_file"
}

func (r *Registry) Run(ctx context.Context, cwd string, call llm.ToolCall) (Result, error) {
	return r.RunWithSandbox(ctx, cwd, "", call)
}

func (r *Registry) RunWithSandbox(ctx context.Context, cwd string, sandbox string, call llm.ToolCall) (Result, error) {
	return r.RunWithPolicy(ctx, cwd, sandbox, "", nil, call)
}

func (r *Registry) RunWithPolicy(ctx context.Context, cwd string, sandbox string, mode string, contextFiles []string, call llm.ToolCall) (Result, error) {
	if r.disabled != nil && r.disabled[call.Name] {
		return Result{}, fmt.Errorf("tool %q is disabled", call.Name)
	}
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
	case "inspect", "plan":
		if isWriteTool(call.Name) {
			return fmt.Errorf("mode %s blocks %s", mode, call.Name)
		}
		if mode == "plan" && isMCPTool(call.Name) {
			return fmt.Errorf("mode plan blocks external MCP tool %s", call.Name)
		}
	case "edit":
		if isFileWriteTool(call.Name) {
			if call.Name == "diff_patch" && len(contextFiles) == 0 {
				return fmt.Errorf("mode edit requires files in context cart before %s", call.Name)
			}
			if len(contextFiles) > 0 {
				if path := primaryWritePath(call); path != "" && !pathAllowed(path, contextFiles) {
					return fmt.Errorf("mode edit blocks %s outside context cart: %s", call.Name, path)
				}
			}
			if call.Name == "write_file" {
				return fmt.Errorf("mode edit blocks legacy write_file; use create_file or focused edit tools")
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

func isProjectSkillBundlePath(path string) bool {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	if path == "" || strings.Contains(path, "../") {
		return false
	}
	for _, prefix := range []string{".klyra/skills/", ".agentcli/skills/", "skills/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if rest == "" || strings.HasPrefix(rest, ".") {
			return false
		}
		parts := strings.Split(rest, "/")
		if len(parts) == 1 {
			return strings.HasSuffix(parts[0], ".md")
		}
		return strings.EqualFold(parts[1], "SKILL.md") || len(parts) > 2
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

func mentionsCodeWorkspace(task string) bool {
	return containsAny(task, []string{
		"repo", "repository", "project", "workspace", "codebase", "file", "directory", "folder",
		"function", "class", "method", "module", "package", "bug", "error", "stack trace",
		"репо", "репозитор", "проект", "воркспейс", "код", "кодовая база", "файл", "папк",
		"директор", "функц", "класс", "метод", "модул", "пакет", "баг", "ошибк", "трейс",
		".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".md", ".json", ".yaml", ".yml",
	})
}

func mentionsSpecificPath(task string) bool {
	for _, field := range strings.Fields(task) {
		field = strings.Trim(field, ".,:;!?()[]{}\"'`")
		lower := strings.ToLower(strings.ReplaceAll(field, "\\", "/"))
		if strings.Contains(lower, "/") && filepathExtLooksUseful(lower) {
			return true
		}
		if filepathExtLooksUseful(lower) {
			return true
		}
	}
	return false
}

func filepathExtLooksUseful(path string) bool {
	for _, ext := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".md", ".json", ".yaml", ".yml", ".css", ".html", ".sh", ".toml"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func mentionsFileListing(task string) bool {
	return containsAny(task, []string{"list files", "show files", "tree", "ls ", "файлы", "список файлов", "структур"})
}

func mentionsGit(task string) bool {
	return containsAny(task, []string{"git", "diff", "status", "commit", "branch", "статус", "дифф", "коммит", "ветк"})
}

func mentionsPlanning(task string) bool {
	return containsAny(task, []string{"plan", "roadmap", "architecture", "design ", "milestone", "план", "роадмап", "архитект", "спроект", "этап"})
}

func mentionsNewFile(task string) bool {
	return containsAny(task, []string{"new file", "create file", "add file", "создай файл", "новый файл", "добавь файл"})
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
		"twitch", "youtube", "github.com", "reddit", "twitter", "x.com",
		"интернет", "в интернете", "веб", "сайт", "ссылк", "url", "актуаль", "последн", "новост", "сегодня", "найди в сети",
		"твитч", "ютуб", "канал", "страниц",
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
