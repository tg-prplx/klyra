package tools

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

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
		DiscoverTools{},
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
		specs = append(specs, compactToolSpec(tool.Spec()))
	}
	sortSpecs(specs)
	return specs
}

func (r *Registry) SpecsForTask(task string) []llm.ToolSpec {
	return r.SpecsForTaskMode(task, "", nil)
}

func (r *Registry) SpecsForTaskMode(task, mode string, contextFiles []string) []llm.ToolSpec {
	return r.SpecsForCapabilities(task, mode, contextFiles, nil)
}

func (r *Registry) SpecsForCapabilities(task, mode string, contextFiles []string, capabilities map[string]bool) []llm.ToolSpec {
	mode = strings.ToLower(strings.TrimSpace(mode))
	explicitPathIntent := mentionsSpecificPath(task)
	names := map[string]bool{"discover_tools": true}

	if hasURL(task) {
		capabilities = withCapability(capabilities, CapabilityWeb)
	}
	if len(contextFiles) > 0 || explicitPathIntent {
		capabilities = withCapability(capabilities, CapabilityWorkspace)
	}
	if mode == "edit" && (len(contextFiles) > 0 || explicitPathIntent) {
		capabilities = withCapability(capabilities, CapabilityEdit)
	}
	switch mode {
	case "plan":
		capabilities = withCapabilities(capabilities, CapabilityWorkspace, CapabilityPlan)
	case "inspect":
		capabilities = withCapability(capabilities, CapabilityWorkspace)
	case "repair":
		capabilities = withCapabilities(capabilities, CapabilityWorkspace, CapabilityGit, CapabilityShell)
	case "refactor":
		capabilities = withCapabilities(capabilities, CapabilityWorkspace, CapabilityEdit, CapabilityGit, CapabilityPlan)
	case "edit":
		names["create_file"] = true
	}
	addCapabilitySpecs(names, capabilities, len(contextFiles) > 0)

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
		delete(names, "workspace_restore")
	case "refactor":
		delete(names, "workspace_restore")
	case "edit":
		if len(contextFiles) == 0 {
			delete(names, "diff_patch")
			delete(names, "write_file")
		}
	}

	specs := make([]llm.ToolSpec, 0, len(names))
	for name := range r.tools {
		if isMCPTool(name) {
			if mode == "plan" || !capabilities[CapabilityExternal] {
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
			specs = append(specs, compactToolSpec(tool.Spec()))
		}
	}
	sortSpecs(specs)
	return specs
}

var compactToolDescriptions = map[string]string{
	"discover_tools":        "Unlock tool groups for this run.",
	"guide":                 "Get brief workflow guidance.",
	"update_plan":           "Update a short task plan.",
	"project_map":           "Show a compact workspace map.",
	"list_files":            "List workspace files.",
	"search":                "Search workspace text.",
	"file_outline":          "Show file symbols.",
	"read_symbol":           "Read one symbol.",
	"read_go_symbol":        "Read one Go symbol.",
	"read_file":             "Read file lines.",
	"create_file":           "Create a new file.",
	"insert_lines":          "Insert file lines.",
	"replace_lines":         "Replace file lines.",
	"replace_symbol":        "Replace one symbol.",
	"diff_preview":          "Validate a patch.",
	"diff_patch":            "Apply a patch.",
	"git_status":            "Show git status.",
	"git_diff":              "Show git diff.",
	"workspace_checkpoint":  "Create a checkpoint.",
	"workspace_checkpoints": "List checkpoints.",
	"workspace_restore":     "Restore a checkpoint.",
	"policy_check":          "Classify a shell command.",
	"bash":                  "Run a shell command.",
	"web_search":            "Search the web.",
	"fetch_url":             "Fetch a URL.",
}

func compactToolSpec(spec llm.ToolSpec) llm.ToolSpec {
	if description := compactToolDescriptions[spec.Name]; description != "" {
		spec.Description = description
	}
	if parameters, ok := compactSchema(spec.Parameters).(map[string]any); ok {
		spec.Parameters = parameters
	}
	return spec
}

func compactSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if key == "description" || key == "title" {
				continue
			}
			out[key] = compactSchema(nested)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = compactSchema(nested)
		}
		return out
	default:
		return value
	}
}

func addCapabilitySpecs(names map[string]bool, capabilities map[string]bool, hasContextCart bool) {
	if capabilities[CapabilityEdit] {
		capabilities = withCapability(capabilities, CapabilityWorkspace)
	}
	if capabilities[CapabilityWorkspace] {
		for _, name := range []string{"guide", "project_map", "search", "list_files", "file_outline", "read_symbol", "read_go_symbol", "read_file"} {
			names[name] = true
		}
	}
	if capabilities[CapabilityEdit] {
		for _, name := range []string{"create_file", "insert_lines", "replace_lines", "replace_symbol"} {
			names[name] = true
		}
		if hasContextCart {
			names["diff_patch"] = true
			names["diff_preview"] = true
			names["workspace_checkpoint"] = true
		}
	}
	if capabilities[CapabilityGit] {
		for _, name := range []string{"git_status", "git_diff", "workspace_checkpoint_list"} {
			names[name] = true
		}
	}
	if capabilities[CapabilityShell] {
		names["policy_check"] = true
		names["bash"] = true
	}
	if capabilities[CapabilityWeb] {
		names["guide"] = true
		names["web_search"] = true
		names["fetch_url"] = true
	}
	if capabilities[CapabilityPlan] {
		names["update_plan"] = true
	}
}

func withCapability(capabilities map[string]bool, capability string) map[string]bool {
	return withCapabilities(capabilities, capability)
}

func withCapabilities(capabilities map[string]bool, values ...string) map[string]bool {
	out := make(map[string]bool, len(capabilities)+len(values))
	for capability, enabled := range capabilities {
		out[capability] = enabled
	}
	for _, capability := range values {
		out[capability] = true
	}
	return out
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

func mentionsSpecificPath(task string) bool {
	for _, field := range strings.Fields(task) {
		field = strings.Trim(field, ".,:;!?()[]{}\"'`")
		lower := strings.ToLower(strings.ReplaceAll(field, "\\", "/"))
		if strings.Contains(lower, "/") && looksLikeFilePath(lower) {
			return true
		}
		if looksLikeFilePath(lower) {
			return true
		}
	}
	return false
}

func looksLikeFilePath(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "" || len(ext) > 12 {
		return false
	}
	for _, char := range ext {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return false
		}
	}
	return true
}

func hasURL(task string) bool {
	for _, field := range strings.Fields(task) {
		field = strings.Trim(field, ".,:;!?()[]{}\"'`")
		parsed, err := url.ParseRequestURI(field)
		if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
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

func SuppressRepeatedSuccessfulCall(name string) bool {
	switch name {
	case "discover_tools", "project_map", "git_status", "git_diff", "workspace_checkpoint_list", "policy_check",
		"list_files", "read_file", "file_outline", "read_symbol", "read_go_symbol", "search":
		return true
	default:
		return false
	}
}

func HasSideEffects(name string) bool {
	return isWriteTool(name) || isMCPTool(name) || name == "workspace_checkpoint"
}
