package tools

import (
	"fmt"
	"strconv"
)

func stringArg(args map[string]any, name string) (string, error) {
	value, ok := args[name]
	if !ok {
		return "", fmt.Errorf("missing argument %q", name)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return text, nil
}

func optionalStringArg(args map[string]any, name string, fallback string) (string, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return fallback, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return text, nil
}

func optionalIntArg(args map[string]any, name string, fallback int) (int, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, fmt.Errorf("argument %q must be an integer", name)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("argument %q must be an integer", name)
	}
}
