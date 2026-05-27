package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agentcli/pkg/llm"
)

type ProjectMap struct{}

func (ProjectMap) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "project_map",
		Description: "Return a token-budgeted repo map with important files, Go symbols, imports, and likely relevant slices. Use this before broad exploration.",
		Parameters: objectSchema(map[string]any{
			"max_files":  integerProperty("Maximum important files to include.", 1),
			"max_tokens": integerProperty("Approximate token budget for the map.", 1),
			"focus":      stringProperty("Optional task/query to rank relevant files and symbols."),
		}),
	}
}

func (ProjectMap) Run(ctx context.Context, inv Invocation) (Result, error) {
	maxFiles, err := optionalIntArg(inv.Args, "max_files", 80)
	if err != nil {
		return Result{}, err
	}
	maxTokens, err := optionalIntArg(inv.Args, "max_tokens", 1000)
	if err != nil {
		return Result{}, err
	}
	focus, _ := inv.Args["focus"].(string)

	var files []string
	byExt := map[string]int{}
	totalBytes := int64(0)
	err = filepath.WalkDir(inv.CWD, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && shouldSkipDir(entry.Name()) && path != inv.CWD {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if shouldSkipFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			totalBytes += info.Size()
		}
		rel, err := filepath.Rel(inv.CWD, path)
		if err != nil {
			return err
		}
		if shouldSkipContextPath(rel, info) {
			return nil
		}
		files = append(files, rel)
		ext := filepath.Ext(rel)
		if ext == "" {
			ext = "[no extension]"
		}
		byExt[ext]++
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Strings(files)
	important := importantFiles(files, maxFiles, focus)
	astSymbols := astSymbolSummaries(ctx, inv.CWD, important, focus)
	var out []string
	out = append(out, fmt.Sprintf("root: %s", inv.CWD))
	out = append(out, fmt.Sprintf("files: %d", len(files)))
	out = append(out, fmt.Sprintf("bytes: %d", totalBytes))
	if strings.TrimSpace(focus) != "" {
		out = append(out, fmt.Sprintf("focus: %s", focus))
	}
	out = append(out, "languages/extensions:")
	for _, pair := range sortedCounts(byExt) {
		out = append(out, fmt.Sprintf("- %s: %d", pair.name, pair.count))
	}
	out = append(out, "important_files:")
	for _, file := range important {
		out = append(out, "- "+file)
	}
	if len(astSymbols) > 0 {
		out = append(out, "ast_symbols:")
		for _, summary := range astSymbols {
			out = append(out, summary.lines()...)
		}
	}
	return Result{Output: trimLinesToTokenBudget(out, maxTokens)}, nil
}

func shouldSkipContextPath(path string, info os.FileInfo) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	name := filepath.Base(lower)
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "go.sum", "cargo.lock", "poetry.lock":
		return true
	}
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") || strings.HasSuffix(name, ".snap") {
		return true
	}
	if strings.Contains(lower, "__snapshots__/") || strings.Contains(name, ".generated.") || strings.Contains(name, ".gen.") {
		return true
	}
	switch filepath.Ext(name) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz", ".tar", ".mp4", ".mov", ".wasm":
		return true
	}
	if info != nil && info.Size() > 256*1024 {
		return true
	}
	return false
}

type countPair struct {
	name  string
	count int
}

func sortedCounts(counts map[string]int) []countPair {
	out := make([]countPair, 0, len(counts))
	for name, count := range counts {
		out = append(out, countPair{name: name, count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count == out[j].count {
			return out[i].name < out[j].name
		}
		return out[i].count > out[j].count
	})
	return out
}

func importantFiles(files []string, limit int, focus string) []string {
	if limit <= 0 || len(files) == 0 {
		return nil
	}
	focusTerms := queryTerms(focus)
	score := func(path string) int {
		name := strings.ToLower(filepath.Base(path))
		dir := strings.ToLower(filepath.Dir(path))
		lowerPath := strings.ToLower(filepath.ToSlash(path))
		score := 0
		switch name {
		case "readme.md", "go.mod", "package.json", "cargo.toml", "pyproject.toml", "implementation_plan.md", "makefile":
			score += 100
		}
		if strings.HasPrefix(dir, "cmd") || strings.HasPrefix(dir, "pkg") || strings.HasPrefix(dir, "src") || strings.HasPrefix(dir, "internal") {
			score += 20
		}
		if strings.Contains(name, "test") {
			score += 8
		}
		if isCodeExtension(filepath.Ext(name)) {
			score += 5
		}
		for _, term := range focusTerms {
			if strings.Contains(lowerPath, term) {
				score += 40
			}
		}
		return score
	}

	sorted := append([]string(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := score(sorted[i]), score(sorted[j])
		if left == right {
			return sorted[i] < sorted[j]
		}
		return left > right
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func queryTerms(query string) []string {
	raw := strings.Fields(strings.ToLower(query))
	var terms []string
	for _, term := range raw {
		term = strings.Trim(term, ".,:;!?()[]{}\"'")
		if len(term) >= 3 {
			terms = append(terms, term)
		}
	}
	return terms
}

func trimLinesToTokenBudget(lines []string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = 1000
	}
	var out []string
	tokens := 0
	for _, line := range lines {
		next := estimateTokens(line) + 1
		if tokens+next > maxTokens {
			out = append(out, fmt.Sprintf("... repo map truncated at ~%d tokens", maxTokens))
			break
		}
		out = append(out, line)
		tokens += next
	}
	return strings.Join(out, "\n")
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text)/4 + 1
}
