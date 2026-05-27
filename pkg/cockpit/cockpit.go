package cockpit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	contextmgr "agentcli/pkg/context"
	"agentcli/pkg/instructions"
	"agentcli/pkg/tools"
)

const (
	DefaultMaxTokens = 1200
	DefaultMaxFiles  = 60
)

type Config struct {
	Enabled         bool
	Inject          bool
	MaxTokens       int
	MaxFiles        int
	IncludeDiff     bool
	IncludeRecipes  bool
	IncludeNegative bool
	MaxInstructions int
}

type Card struct {
	Kind      string
	Title     string
	Reason    string
	Freshness string
	Tokens    int
	Content   string
}

type Snapshot struct {
	Enabled         bool
	Injected        bool
	MaxTokens       int
	EstimatedTokens int
	Cards           []Card
	Warnings        []string
}

func Build(ctx context.Context, cfg Config, cwd, focus string, contextFiles []string) (Snapshot, error) {
	cfg = normalizeConfig(cfg)
	snapshot := Snapshot{
		Enabled:   cfg.Enabled,
		Injected:  cfg.Inject,
		MaxTokens: cfg.MaxTokens,
	}
	if !cfg.Enabled {
		return snapshot, nil
	}

	now := time.Now().Format("15:04:05")
	addCard := func(kind, title, reason, content string) {
		content = trimToTokenBudget(strings.TrimSpace(content), cardBudget(cfg.MaxTokens))
		if content == "" {
			return
		}
		card := Card{
			Kind:      kind,
			Title:     title,
			Reason:    reason,
			Freshness: "built " + now,
			Content:   content,
		}
		card.Tokens = contextmgr.EstimateTokens(card.Content)
		snapshot.Cards = append(snapshot.Cards, card)
	}

	repoMap, err := (tools.ProjectMap{}).Run(ctx, tools.Invocation{
		CWD: cwd,
		Args: map[string]any{
			"max_files":  cfg.MaxFiles,
			"max_tokens": max(250, cfg.MaxTokens*55/100),
			"focus":      focus,
		},
	})
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "project_map: "+err.Error())
	} else {
		addCard("repo_map", "Repo Map", "ranked files, symbols, imports, and signatures for the current task", repoMap.Output)
	}

	addCard("aci", "Agent Rails", "commands weak/local models should prefer for small safe steps", strings.Join([]string{
		"- start broad work with project_map around 1000 tokens",
		"- use search before opening files",
		"- use read_go_symbol for Go declarations when possible",
		"- use read_file with start_line and about 100 lines instead of dumping files",
		"- use git_status/git_diff before edits and tests after edits",
		"- prefer diff_patch for changes; write_file only for new or complete files",
		"- treat Negative Context as a deny-list unless the user explicitly asks for those files",
	}, "\n"))

	if cfg.IncludeRecipes {
		scoped, err := instructions.LoadScoped(cwd, focus, contextFiles, cfg.MaxInstructions/2)
		if err != nil {
			snapshot.Warnings = append(snapshot.Warnings, "scoped recipes: "+err.Error())
		} else if len(scoped.Files) > 0 {
			var lines []string
			for _, file := range scoped.Files {
				suffix := ""
				if file.Truncated {
					suffix = " truncated"
				}
				lines = append(lines, fmt.Sprintf("- %s (%d bytes%s): %s", file.Path, file.Bytes, suffix, file.Reason))
			}
			if scoped.Truncated {
				lines = append(lines, "- scoped recipes truncated by configured byte budget")
			}
			addCard("recipes", "Context Recipes", "scoped rules matched by task, AST-ish path hints, and context cart files", strings.Join(lines, "\n"))
		}
	}

	if len(contextFiles) > 0 {
		var lines []string
		for _, file := range contextFiles {
			lines = append(lines, "- "+file)
		}
		addCard("cart", "Context Cart", "explicit files allowed for edit/refactor tools", strings.Join(lines, "\n"))
	}

	status, err := (tools.GitStatus{}).Run(ctx, tools.Invocation{CWD: cwd, Args: map[string]any{}})
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "git_status: "+err.Error())
	} else if strings.TrimSpace(status.Output) != "" {
		addCard("git_status", "Workspace Changes", "current dirty state that can affect edits and tests", status.Output)
	}

	if cfg.IncludeDiff {
		diff, err := (tools.GitDiff{}).Run(ctx, tools.Invocation{CWD: cwd, Args: map[string]any{"max_lines": 80}})
		if err != nil {
			snapshot.Warnings = append(snapshot.Warnings, "git_diff: "+err.Error())
		} else if strings.TrimSpace(diff.Output) != "" && strings.TrimSpace(diff.Output) != "no tracked diff" {
			addCard("diff", "Related Diff", "tracked changes already present in the workspace", diff.Output)
		}
	}

	loaded, err := instructions.Load(cwd, cfg.MaxInstructions)
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "instructions: "+err.Error())
	} else if len(loaded.Files) > 0 {
		var lines []string
		for _, file := range loaded.Files {
			suffix := ""
			if file.Truncated {
				suffix = " truncated"
			}
			lines = append(lines, fmt.Sprintf("- %s (%d bytes%s)", file.Path, file.Bytes, suffix))
		}
		if loaded.Truncated {
			lines = append(lines, "- instruction set truncated by configured byte budget")
		}
		addCard("rules", "Project Rules", "instruction sources already loaded into the system prompt", strings.Join(lines, "\n"))
	}

	if cfg.IncludeNegative {
		if negative := detectNegativeContext(cwd, 40); strings.TrimSpace(negative) != "" {
			addCard("negative_context", "Negative Context", "files intentionally withheld from model context to avoid token waste and distraction", negative)
		}
	}

	snapshot = trimSnapshot(snapshot, cfg.MaxTokens)
	return snapshot, nil
}

func (s Snapshot) Markdown() string {
	if !s.Enabled {
		return "context cockpit disabled"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "- budget: `%d tokens`\n", s.MaxTokens)
	fmt.Fprintf(&builder, "- estimated: `%d tokens`\n", s.EstimatedTokens)
	fmt.Fprintf(&builder, "- inject: `%s`\n", onOff(s.Injected))
	for _, warning := range s.Warnings {
		fmt.Fprintf(&builder, "- warning: `%s`\n", warning)
	}
	for _, card := range s.Cards {
		fmt.Fprintf(&builder, "\n%s\n\n", card.Title)
		fmt.Fprintf(&builder, "- kind: `%s`\n", card.Kind)
		fmt.Fprintf(&builder, "- why: %s\n", card.Reason)
		fmt.Fprintf(&builder, "- freshness: `%s`\n", card.Freshness)
		fmt.Fprintf(&builder, "- tokens: `%d`\n\n", card.Tokens)
		builder.WriteString("```text\n")
		builder.WriteString(strings.TrimSpace(card.Content))
		builder.WriteString("\n```\n")
	}
	return strings.TrimSpace(builder.String())
}

func normalizeConfig(cfg Config) Config {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = DefaultMaxFiles
	}
	if cfg.MaxInstructions <= 0 {
		cfg.MaxInstructions = instructions.DefaultMaxBytes
	}
	return cfg
}

func trimSnapshot(snapshot Snapshot, maxTokens int) Snapshot {
	for {
		total := 0
		for i := range snapshot.Cards {
			snapshot.Cards[i].Tokens = contextmgr.EstimateTokens(snapshot.Cards[i].Content)
			total += snapshot.Cards[i].Tokens
		}
		snapshot.EstimatedTokens = total
		if total <= maxTokens || len(snapshot.Cards) == 0 {
			return snapshot
		}
		snapshot.Cards = snapshot.Cards[:len(snapshot.Cards)-1]
	}
}

func cardBudget(total int) int {
	if total <= 0 {
		return 300
	}
	return max(120, total/2)
}

func trimToTokenBudget(text string, maxTokens int) string {
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && contextmgr.EstimateTokens(strings.Join(lines, "\n")) > maxTokens {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func detectNegativeContext(cwd string, limit int) string {
	type blocked struct {
		path   string
		reason string
	}
	var blockedFiles []blocked
	_ = filepath.WalkDir(cwd, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != cwd {
				if reason := denyDirReason(entry.Name()); reason != "" {
					rel, relErr := filepath.Rel(cwd, path)
					if relErr == nil {
						blockedFiles = append(blockedFiles, blocked{path: filepath.ToSlash(rel) + "/", reason: reason})
					}
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(cwd, path)
		if relErr != nil {
			return nil
		}
		reason := denyFileReason(filepath.ToSlash(rel), entry)
		if reason != "" {
			blockedFiles = append(blockedFiles, blocked{path: filepath.ToSlash(rel), reason: reason})
		}
		return nil
	})
	sort.Slice(blockedFiles, func(i, j int) bool {
		if blockedFiles[i].reason == blockedFiles[j].reason {
			return blockedFiles[i].path < blockedFiles[j].path
		}
		return blockedFiles[i].reason < blockedFiles[j].reason
	})
	if len(blockedFiles) > limit {
		blockedFiles = append(blockedFiles[:limit], blocked{path: fmt.Sprintf("... %d more", len(blockedFiles)-limit), reason: "hidden"})
	}
	var lines []string
	for _, file := range blockedFiles {
		lines = append(lines, fmt.Sprintf("- %s: %s", file.path, file.reason))
	}
	return strings.Join(lines, "\n")
}

func denyDirReason(name string) string {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "vendor":
		return "vendored/dependency directory"
	case "dist", "build", "coverage", ".next", ".nuxt", ".cache", "target", "out":
		return "generated build output"
	default:
		return ""
	}
}

func denyFileReason(path string, entry os.DirEntry) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	name := strings.ToLower(filepath.Base(lower))
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") || strings.Contains(name, ".generated.") || strings.Contains(name, ".gen.") {
		return "generated/minified asset"
	}
	if strings.HasSuffix(name, ".snap") || strings.Contains(lower, "__snapshots__/") {
		return "test snapshot"
	}
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "go.sum", "cargo.lock", "poetry.lock":
		return "lockfile"
	}
	switch filepath.Ext(name) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz", ".tar", ".mp4", ".mov", ".wasm":
		return "binary/large asset"
	case ".json":
		if info, err := entry.Info(); err == nil && info.Size() > 64*1024 {
			return "large json"
		}
	}
	if info, err := entry.Info(); err == nil && info.Size() > 256*1024 {
		return "large file"
	}
	if strings.Contains(lower, "/migrations/") && oldMigrationName(name) {
		return "old migration"
	}
	return ""
}

func oldMigrationName(name string) bool {
	if len(name) < 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	return true
}
