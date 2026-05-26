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
		Description: "Return a compact workspace map with language counts and likely important files. Use this before broad exploration.",
		Parameters: objectSchema(map[string]any{
			"max_files": integerProperty("Maximum important files to include.", 1),
		}),
	}
}

func (ProjectMap) Run(_ context.Context, inv Invocation) (Result, error) {
	maxFiles, err := optionalIntArg(inv.Args, "max_files", 80)
	if err != nil {
		return Result{}, err
	}

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
	important := importantFiles(files, maxFiles)
	var out []string
	out = append(out, fmt.Sprintf("root: %s", inv.CWD))
	out = append(out, fmt.Sprintf("files: %d", len(files)))
	out = append(out, fmt.Sprintf("bytes: %d", totalBytes))
	out = append(out, "languages/extensions:")
	for _, pair := range sortedCounts(byExt) {
		out = append(out, fmt.Sprintf("- %s: %d", pair.name, pair.count))
	}
	out = append(out, "important_files:")
	for _, file := range important {
		out = append(out, "- "+file)
	}
	return Result{Output: strings.Join(out, "\n")}, nil
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

func importantFiles(files []string, limit int) []string {
	if limit <= 0 || len(files) == 0 {
		return nil
	}
	score := func(path string) int {
		name := strings.ToLower(filepath.Base(path))
		dir := strings.ToLower(filepath.Dir(path))
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
		if filepath.Ext(name) == ".go" || filepath.Ext(name) == ".rs" || filepath.Ext(name) == ".ts" || filepath.Ext(name) == ".py" {
			score += 5
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
