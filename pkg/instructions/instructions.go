package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultMaxBytes = 12_000

type File struct {
	Path      string
	Bytes     int
	Truncated bool
}

type Result struct {
	Content   string
	Files     []File
	Bytes     int
	Truncated bool
}

type ScopedFile struct {
	Path      string
	Bytes     int
	Truncated bool
	Reason    string
}

type ScopedResult struct {
	Content   string
	Files     []ScopedFile
	Bytes     int
	Truncated bool
}

func Load(cwd string, maxBytes int) (Result, error) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return Result{}, err
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	paths, err := candidatePaths(root)
	if err != nil {
		return Result{}, err
	}

	var result Result
	var sections []string
	for _, path := range paths {
		if result.Bytes >= maxBytes {
			result.Truncated = true
			break
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Result{}, fmt.Errorf("read project instructions %s: %w", path, err)
		}
		content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		if content == "" {
			continue
		}
		remaining := maxBytes - result.Bytes
		truncated := false
		if len(content) > remaining {
			content = trimToBytes(content, remaining)
			truncated = true
			result.Truncated = true
		}
		result.Files = append(result.Files, File{Path: path, Bytes: len(content), Truncated: truncated})
		result.Bytes += len(content)
		sections = append(sections, fmt.Sprintf("Source: %s\n%s", path, content))
	}
	if len(sections) > 0 {
		result.Content = strings.Join(sections, "\n\n")
	}
	return result, nil
}

func candidatePaths(root string) ([]string, error) {
	ordered := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"GEMINI.md",
		"CONTEXT.md",
		".agentcli/instructions.md",
		".agentcli/rules.md",
		".cursorrules",
		".github/copilot-instructions.md",
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range ordered {
		if fileExists(filepath.Join(root, path)) {
			out = append(out, filepath.ToSlash(path))
			seen[filepath.ToSlash(path)] = true
		}
	}

	cursorRules, err := filepath.Glob(filepath.Join(root, ".cursor", "rules", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(cursorRules)
	for _, path := range cursorRules {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			out = append(out, rel)
			seen[rel] = true
		}
	}
	return out, nil
}

func LoadScoped(cwd, focus string, contextFiles []string, maxBytes int) (ScopedResult, error) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return ScopedResult{}, err
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes / 2
	}
	paths, err := scopedCandidatePaths(root)
	if err != nil {
		return ScopedResult{}, err
	}
	terms := scopedTerms(focus, contextFiles)
	if len(terms) == 0 {
		return ScopedResult{}, nil
	}

	var result ScopedResult
	var sections []string
	for _, path := range paths {
		reason := matchScopedRule(path, terms)
		if reason == "" {
			continue
		}
		if result.Bytes >= maxBytes {
			result.Truncated = true
			break
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return ScopedResult{}, fmt.Errorf("read scoped instruction %s: %w", path, err)
		}
		content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		if content == "" {
			continue
		}
		remaining := maxBytes - result.Bytes
		truncated := false
		if len(content) > remaining {
			content = trimToBytes(content, remaining)
			truncated = true
			result.Truncated = true
		}
		result.Files = append(result.Files, ScopedFile{Path: path, Bytes: len(content), Truncated: truncated, Reason: reason})
		result.Bytes += len(content)
		sections = append(sections, fmt.Sprintf("Scoped rule: %s\nReason: %s\n%s", path, reason, content))
	}
	if len(sections) > 0 {
		result.Content = strings.Join(sections, "\n\n")
	}
	return result, nil
}

func scopedCandidatePaths(root string) ([]string, error) {
	patterns := []string{
		".klyra/rules/*.md",
		".klyra/recipes/*.md",
		".agentcli/context/*.md",
		".agentcli/recipes/*.md",
		".agentcli/rules/*.md",
		".cursor/rules/*.md",
	}
	seen := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		for _, path := range matches {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if !seen[rel] {
				out = append(out, rel)
				seen[rel] = true
			}
		}
	}
	return out, nil
}

func scopedTerms(focus string, contextFiles []string) map[string]string {
	terms := map[string]string{}
	add := func(term, reason string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if len(term) < 3 {
			return
		}
		if _, ok := terms[term]; !ok {
			terms[term] = reason
		}
	}
	for _, raw := range strings.FieldsFunc(strings.ToLower(focus), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		add(raw, "task mentions "+raw)
	}
	for _, file := range contextFiles {
		path := strings.ToLower(filepath.ToSlash(file))
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		for _, part := range strings.FieldsFunc(path+" "+base, func(r rune) bool {
			return r == '/' || r == '-' || r == '_' || r == '.' || r == ' '
		}) {
			add(part, "context path matches "+file)
		}
		switch filepath.Ext(path) {
		case ".tsx", ".jsx", ".css", ".scss", ".vue", ".svelte":
			add("frontend", "frontend file "+file)
			add("react", "frontend file "+file)
			add("style", "frontend file "+file)
		case ".sql":
			add("database", "database file "+file)
			add("migration", "database file "+file)
		}
		if strings.Contains(path, "migration") || strings.Contains(path, "schema") {
			add("migration", "migration/schema path "+file)
			add("database", "migration/schema path "+file)
		}
		if strings.Contains(path, "test") || strings.Contains(path, "spec") {
			add("testing", "test/spec path "+file)
			add("test", "test/spec path "+file)
		}
		if strings.Contains(path, "api") || strings.Contains(path, "handler") || strings.Contains(path, "route") {
			add("backend", "backend/api path "+file)
			add("api", "backend/api path "+file)
		}
	}
	return terms
}

func matchScopedRule(path string, terms map[string]string) string {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.TrimSuffix(filepath.Base(lower), filepath.Ext(lower))
	for term, reason := range terms {
		if strings.Contains(lower, term) || strings.Contains(base, term) {
			return reason
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func trimToBytes(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	marker := "[truncated]"
	if maxBytes <= len(marker) {
		return marker[:maxBytes]
	}
	limit := maxBytes - len(marker) - 1
	content = content[:limit]
	if idx := strings.LastIndexByte(content, '\n'); idx > limit/2 {
		content = content[:idx]
	}
	out := strings.TrimSpace(content) + "\n" + marker
	if len(out) > maxBytes {
		return out[:maxBytes]
	}
	return out
}
