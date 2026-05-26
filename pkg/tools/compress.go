package tools

import (
	"regexp"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func CompressOutput(raw string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 120
	}

	cleaned := ansiPattern.ReplaceAllString(raw, "")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	cleaned = regexp.MustCompile(`\r[^\n]*`).ReplaceAllString(cleaned, "")

	lines := splitNonEmptyLines(cleaned)
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}

	important := selectImportantLines(lines, maxLines/2)
	headCount := maxLines / 4
	tailCount := maxLines - headCount - len(important) - 1
	if tailCount < 0 {
		tailCount = 0
	}

	out := make([]string, 0, maxLines)
	out = append(out, lines[:headCount]...)
	out = append(out, important...)
	out = append(out, "... output compressed ...")
	out = append(out, lines[len(lines)-tailCount:]...)
	return strings.Join(out, "\n")
}

func splitNonEmptyLines(text string) []string {
	rawLines := strings.Split(strings.TrimSpace(text), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func selectImportantLines(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	matcher := regexp.MustCompile(`(?i)(error|failed|failure|panic|fatal|exception|traceback|denied|timeout)`)
	selected := make([]string, 0, limit)
	for _, line := range lines {
		if matcher.MatchString(line) {
			selected = append(selected, line)
			if len(selected) >= limit {
				return selected
			}
		}
	}
	return selected
}
