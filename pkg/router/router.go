package router

import "strings"

type Class string

const (
	ClassFast Class = "fast"
	ClassEdit Class = "edit"
	ClassDeep Class = "deep"
)

type ModelRoutes map[string]string

func Classify(mode string) Class {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan", "refactor":
		return ClassDeep
	case "edit", "repair":
		return ClassEdit
	default:
		return ClassFast
	}
}

func SelectModel(defaultModel string, routes ModelRoutes, mode string) string {
	if routes == nil {
		return defaultModel
	}
	class := string(Classify(mode))
	if model := strings.TrimSpace(routes[class]); model != "" {
		return model
	}
	if model := strings.TrimSpace(routes["default"]); model != "" {
		return model
	}
	return defaultModel
}
