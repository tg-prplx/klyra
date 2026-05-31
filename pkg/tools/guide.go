package tools

import (
	"context"
	"fmt"
	"strings"

	"klyra/pkg/llm"
	"klyra/pkg/skills"
)

type Guide struct{}

func (Guide) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "guide",
		Description: "Return a compact task-specific workflow before unfamiliar work. Call at most once per user request, then act with a task tool or answer.",
		Parameters: objectSchema(map[string]any{
			"query": stringProperty("Short description of the task or workflow guidance needed."),
		}, "query"),
	}
}

func (Guide) Run(_ context.Context, inv Invocation) (Result, error) {
	query, err := stringArg(inv.Args, "query")
	if err != nil {
		return Result{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, fmt.Errorf("query cannot be empty")
	}
	var sections []string
	if text := builtInGuide(query); text != "" {
		sections = append(sections, text)
	}
	if matched, err := skills.Load(inv.CWD, query, inv.ContextFiles, 2400); err == nil && strings.TrimSpace(matched.Content) != "" {
		sections = append(sections, "Matched project skills:\n"+strings.TrimSpace(matched.Content))
	}
	if len(sections) == 0 {
		sections = append(sections, genericGuide())
	}
	return Result{Output: strings.Join(sections, "\n\n")}, nil
}

func builtInGuide(query string) string {
	lower := strings.ToLower(query)
	switch {
	case mentionsSkill(lower):
		return strings.TrimSpace(`Skill creation workflow:
1. Create exactly one markdown file under .klyra/skills/<short-name>.md, .klyra/skills/<short-name>/SKILL.md, skills/<short-name>.md, or skills/<short-name>/SKILL.md.
2. If supporting scripts/examples are genuinely needed, create them under that same skill directory only.
3. Use create_file for new skill files. Do not inspect sessions, .env, or unrelated project files unless the user explicitly asks.
4. Include metadata at the top: name, description, triggers.
5. Keep the body short, operational, and task-specific. Mention exact tools or commands the future agent should use.
6. The new skill is loaded on the next user request, not the current one.`)
	case mentionsWeb(lower) || strings.Contains(lower, "issue") || strings.Contains(lower, "github"):
		return strings.TrimSpace(`Web and issue workflow:
1. Use web_search only to find candidate pages, then fetch_url with query/focus to retrieve relevant chunks.
2. For long pages, pass max_tokens and a focused query; avoid fetching whole pages into the prompt.
3. Summarize from fetched evidence and include concrete links or identifiers when available.
4. Do not use shell/network workarounds unless the built-in web tools cannot answer the request.`)
	case mentionsEdit(lower):
		return strings.TrimSpace(`Edit workflow:
1. Prefer focused tools: read_symbol/file_outline for context, then replace_symbol, replace_lines, insert_lines, create_file, or diff_patch.
2. Avoid broad bash/find/session scans unless file discovery tools are insufficient.
3. Verify with the narrowest relevant test first, then wider tests if the touched surface is shared.
4. Keep unrelated dirty files untouched.`)
	default:
		return ""
	}
}

func genericGuide() string {
	return strings.TrimSpace(`General workflow:
1. Identify the smallest useful context slice before acting.
2. Prefer project_map, file_outline, read_symbol, search, and fetch_url focused retrieval over broad file dumps.
3. Use write tools only after the target file/path is clear.
4. Verify the change with focused tests or commands relevant to the task.`)
}
