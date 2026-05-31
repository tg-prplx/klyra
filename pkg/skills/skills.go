package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const DefaultMaxBytes = 6000

type Skill struct {
	Path        string
	Name        string
	Description string
	Triggers    []string
	Content     string
	Bytes       int
	Truncated   bool
	Reason      string
}

type Result struct {
	Skills    []Skill
	Content   string
	Bytes     int
	Truncated bool
}

func Load(cwd, focus string, contextFiles []string, maxBytes int) (Result, error) {
	all, err := LoadAll(cwd, 0)
	if err != nil {
		return Result{}, err
	}
	terms := matchTerms(focus, contextFiles)
	if len(terms) == 0 {
		return Result{}, nil
	}
	var matched []Skill
	for _, skill := range all.Skills {
		reason := matchSkill(skill, terms)
		if reason == "" {
			continue
		}
		skill.Reason = reason
		matched = append(matched, skill)
	}
	return pack(matched, normalizeBudget(maxBytes))
}

func LoadAll(cwd string, maxBytes int) (Result, error) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return Result{}, err
	}
	paths, err := candidatePaths(root)
	if err != nil {
		return Result{}, err
	}
	var skills []Skill
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Result{}, fmt.Errorf("read skill %s: %w", path, err)
		}
		content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		if content == "" {
			continue
		}
		skill := parseSkill(path, content)
		skills = append(skills, skill)
	}
	return pack(skills, normalizeBudget(maxBytes))
}

func pack(skills []Skill, maxBytes int) (Result, error) {
	var result Result
	var sections []string
	for _, skill := range skills {
		if result.Bytes >= maxBytes {
			result.Truncated = true
			break
		}
		content := skill.Content
		remaining := maxBytes - result.Bytes
		if len(content) > remaining {
			content = trimToBytes(content, remaining)
			skill.Truncated = true
			result.Truncated = true
		}
		skill.Content = content
		skill.Bytes = len(content)
		result.Bytes += skill.Bytes
		result.Skills = append(result.Skills, skill)
		sections = append(sections, skillSection(skill))
	}
	if len(sections) > 0 {
		result.Content = strings.Join(sections, "\n\n")
	}
	return result, nil
}

func parseSkill(path, content string) Skill {
	skill := Skill{
		Path:    filepath.ToSlash(path),
		Name:    fallbackName(path),
		Content: content,
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && trimmed == "---" {
			continue
		}
		if trimmed == "---" && i > 0 {
			break
		}
		if strings.HasPrefix(trimmed, "# ") && skill.Name == fallbackName(path) {
			skill.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name", "title":
			if strings.TrimSpace(value) != "" {
				skill.Name = stripQuotes(value)
			}
		case "description", "summary":
			skill.Description = stripQuotes(value)
		case "triggers", "trigger", "tags":
			skill.Triggers = splitList(value)
		}
	}
	return skill
}

func skillSection(skill Skill) string {
	var lines []string
	lines = append(lines, "Skill: "+skill.Name)
	lines = append(lines, "Source: "+skill.Path)
	if skill.Description != "" {
		lines = append(lines, "Description: "+skill.Description)
	}
	if len(skill.Triggers) > 0 {
		lines = append(lines, "Triggers: "+strings.Join(skill.Triggers, ", "))
	}
	if skill.Reason != "" {
		lines = append(lines, "Reason: "+skill.Reason)
	}
	lines = append(lines, strings.TrimSpace(skill.Content))
	return strings.Join(lines, "\n")
}

func candidatePaths(root string) ([]string, error) {
	patterns := []string{
		".klyra/skills/*.md",
		".klyra/skills/*/SKILL.md",
		".agentcli/skills/*.md",
		".agentcli/skills/*/SKILL.md",
		"skills/*.md",
		"skills/*/SKILL.md",
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

func matchTerms(focus string, contextFiles []string) map[string]string {
	terms := map[string]string{}
	add := func(term, reason string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if len(term) < 3 {
			return
		}
		if _, ok := terms[term]; !ok {
			terms[term] = reason
		}
		if len(term) > 4 && strings.HasSuffix(term, "s") {
			singular := strings.TrimSuffix(term, "s")
			if _, ok := terms[singular]; !ok {
				terms[singular] = reason
			}
		}
	}
	for _, raw := range strings.FieldsFunc(strings.ToLower(focus), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
	}) {
		add(raw, "task mentions "+raw)
	}
	for _, file := range contextFiles {
		path := strings.ToLower(filepath.ToSlash(file))
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		for _, part := range strings.FieldsFunc(path+" "+base, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}) {
			add(part, "context path matches "+file)
		}
	}
	return terms
}

func matchSkill(skill Skill, terms map[string]string) string {
	haystacks := []string{skill.Path, skill.Name, skill.Description}
	haystacks = append(haystacks, skill.Triggers...)
	for term, reason := range terms {
		for _, text := range haystacks {
			if strings.Contains(strings.ToLower(text), term) {
				return reason
			}
		}
	}
	return ""
}

func fallbackName(path string) string {
	base := filepath.Base(path)
	if strings.EqualFold(base, "SKILL.md") {
		base = filepath.Base(filepath.Dir(path))
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func splitList(value string) []string {
	value = stripQuotes(value)
	value = strings.Trim(value, "[]")
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	})
	var out []string
	for _, field := range fields {
		field = stripQuotes(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func stripQuotes(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func normalizeBudget(maxBytes int) int {
	if maxBytes <= 0 {
		return DefaultMaxBytes
	}
	return maxBytes
}

func trimToBytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	return strings.TrimSpace(text[:maxBytes]) + "\n[truncated]"
}
