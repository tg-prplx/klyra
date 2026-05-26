package router

import "strings"

type Class string

const (
	ClassFast Class = "fast"
	ClassEdit Class = "edit"
	ClassDeep Class = "deep"
)

type ModelRoutes map[string]string

func Classify(task string) Class {
	text := strings.ToLower(task)
	if containsAny(text, []string{"architecture", "design", "проектирован", "архитект", "security", "безопас", "audit", "аудит", "large refactor", "deep"}) {
		return ClassDeep
	}
	if containsAny(text, []string{
		"implement", "add ", "fix", "change", "edit", "write", "refactor", "delete",
		"реализ", "добав", "исправ", "измени", "поправ", "напиши", "удали", "рефактор",
	}) {
		return ClassEdit
	}
	return ClassFast
}

func SelectModel(defaultModel string, routes ModelRoutes, task string) string {
	if routes == nil {
		return defaultModel
	}
	class := string(Classify(task))
	if model := strings.TrimSpace(routes[class]); model != "" {
		return model
	}
	if model := strings.TrimSpace(routes["default"]); model != "" {
		return model
	}
	return defaultModel
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
