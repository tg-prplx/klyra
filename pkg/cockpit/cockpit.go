package cockpit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	contextmgr "klyra/pkg/context"
	"klyra/pkg/instructions"
	"klyra/pkg/tools"
)

const (
	DefaultMaxTokens = 1200
	DefaultMaxFiles  = 60
	DefaultMaxCards  = 10
)

type Config struct {
	Enabled          bool
	Inject           bool
	MaxTokens        int
	MaxFiles         int
	MaxCards         int
	IncludeDiff      bool
	IncludeRecipes   bool
	IncludeNegative  bool
	IncludeRetrieval bool
	RetrievalTokens  int
	RetrievalChunks  int
	UseEmbeddings    bool
	UseReranker      bool
	MaxInstructions  int
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
	addCardBudget := func(kind, title, reason, content string, budget int) {
		content = trimToTokenBudget(strings.TrimSpace(content), budget)
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
	addCard := func(kind, title, reason, content string) {
		addCardBudget(kind, title, reason, content, cardBudget(cfg.MaxTokens))
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
		addCard("repo_map", "Repo Map", "ranked files and symbols for the task", repoMap.Output)
	}

	if cfg.IncludeRetrieval && strings.TrimSpace(focus) != "" {
		retrieval, warnings := buildRetrievalCart(ctx, retrievalConfig{
			MaxTokens:     cfg.RetrievalTokens,
			MaxChunks:     cfg.RetrievalChunks,
			MaxFiles:      cfg.MaxFiles,
			UseEmbeddings: cfg.UseEmbeddings,
			UseReranker:   cfg.UseReranker,
			RepoMap:       repoMap.Output,
		}, cwd, focus)
		snapshot.Warnings = append(snapshot.Warnings, warnings...)
		addCardBudget("retrieval_cart", "Retrieval Cart", "BM25 chunks boosted by local embeddings and AST repo-map hints; selected context with token prices", retrieval, cfg.RetrievalTokens)
	}

	addCard("aci", "Agent Rails", "preferred low-token workflow", strings.Join([]string{
		"- map/search/outline before reading files",
		"- read one symbol or about 100 lines",
		"- edit existing files with replace_symbol/replace_lines/insert_lines/diff_patch",
		"- create new files with create_file; never rewrite existing files from scratch",
		"- check git diff and run focused tests after edits",
		"- do not open Negative Context unless asked",
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
			addCard("recipes", "Context Recipes", "scoped rules matched for this task", strings.Join(lines, "\n"))
		}
	}

	if len(contextFiles) > 0 {
		var lines []string
		for _, file := range contextFiles {
			lines = append(lines, "- "+file)
		}
		addCard("cart", "Context Cart", "files allowed for edit/refactor tools", strings.Join(lines, "\n"))
	}

	status, err := (tools.GitStatus{}).Run(ctx, tools.Invocation{CWD: cwd, Args: map[string]any{}})
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "git_status: "+err.Error())
	} else if strings.TrimSpace(status.Output) != "" {
		addCard("git_status", "Workspace Changes", "current dirty state", status.Output)
	}

	if cfg.IncludeDiff {
		diff, err := (tools.GitDiff{}).Run(ctx, tools.Invocation{CWD: cwd, Args: map[string]any{"max_lines": 80}})
		if err != nil {
			snapshot.Warnings = append(snapshot.Warnings, "git_diff: "+err.Error())
		} else if strings.TrimSpace(diff.Output) != "" && strings.TrimSpace(diff.Output) != "no tracked diff" {
			addCard("diff", "Related Diff", "tracked changes already present", diff.Output)
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
		addCard("rules", "Project Rules", "instruction sources loaded into the system prompt", strings.Join(lines, "\n"))
	}

	if cfg.IncludeNegative {
		if negative := detectNegativeContext(cwd, 40); strings.TrimSpace(negative) != "" {
			addCard("negative_context", "Negative Context", "files withheld to save tokens", negative)
		}
	}

	snapshot = trimSnapshot(snapshot, cfg.MaxTokens, cfg.MaxCards)
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

func (s Snapshot) PromptText() string {
	if !s.Enabled {
		return ""
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "budget=%d estimated=%d\n", s.MaxTokens, s.EstimatedTokens)
	for _, warning := range s.Warnings {
		fmt.Fprintf(&builder, "warning: %s\n", warning)
	}
	for _, card := range s.Cards {
		fmt.Fprintf(&builder, "\n[%s] %s\n", card.Title, card.Reason)
		builder.WriteString(strings.TrimSpace(card.Content))
		builder.WriteString("\n")
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
	if cfg.MaxCards <= 0 {
		cfg.MaxCards = DefaultMaxCards
	}
	if cfg.RetrievalTokens <= 0 {
		cfg.RetrievalTokens = min(1000, max(350, cfg.MaxTokens*2/3))
	}
	if cfg.RetrievalChunks <= 0 {
		cfg.RetrievalChunks = 10
	}
	if cfg.MaxInstructions <= 0 {
		cfg.MaxInstructions = instructions.DefaultMaxBytes
	}
	return cfg
}

func trimSnapshot(snapshot Snapshot, maxTokens, maxCards int) Snapshot {
	if maxCards <= 0 {
		maxCards = DefaultMaxCards
	}
	for {
		if len(snapshot.Cards) > maxCards {
			snapshot.Cards = snapshot.Cards[:maxCards]
		}
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
	return ""
}
