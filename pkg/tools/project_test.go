package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectMapReturnsCompactImportantFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module sample\n")
	writeTestFile(t, dir, "cmd/app/main.go", `package main

import "fmt"

type Server struct{}

func main() {}

func (s *Server) Login(user string) error {
	fmt.Println(user)
	return nil
}
`)
	writeTestFile(t, dir, "node_modules/ignored/index.js", "ignored\n")
	writeTestFile(t, dir, "go.sum", "hash\n")
	writeTestFile(t, dir, "web/app.min.js", "minified\n")
	writeTestFile(t, dir, "web/component.tsx", `import React from "react";

export interface Props {
	name: string
}

export function UserCard(props: Props) {
	return <section>{props.name}</section>
}
`)
	writeTestFile(t, dir, "scripts/tasks.py", `import pathlib

class Runner:
    def run(self):
        return pathlib.Path(".")
`)

	result, err := ProjectMap{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"max_files": 10, "max_tokens": 220, "focus": "login"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "go.mod") || !strings.Contains(result.Output, "cmd/app/main.go") {
		t.Fatalf("expected important files in project map:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "node_modules") {
		t.Fatalf("expected generated directories to be skipped:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "go.sum") || strings.Contains(result.Output, "app.min.js") {
		t.Fatalf("expected negative context files to be skipped:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "ast_symbols:") || !strings.Contains(result.Output, "method Login") {
		t.Fatalf("expected Go symbols in project map:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "language=tsx") || !strings.Contains(result.Output, "UserCard") {
		t.Fatalf("expected TSX tree-sitter symbols in project map:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "language=python") || !strings.Contains(result.Output, "Runner") {
		t.Fatalf("expected Python tree-sitter symbols in project map:\n%s", result.Output)
	}
}

func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
