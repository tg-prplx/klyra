package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"klyra/pkg/llm"
)

type FileOutline struct{}

func (FileOutline) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "file_outline",
		Description: "Compact AST outline for one file: language, imports, symbols. Use before read_file.",
		Parameters: objectSchema(map[string]any{
			"path": stringProperty(workspacePathArgDescription),
		}, "path"),
	}
}

func (FileOutline) Run(ctx context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	parseCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	summary, err := parseASTFileSummary(parseCtx, target, filepath.ToSlash(requestedPath), nil)
	if err != nil {
		return Result{}, err
	}
	if summary.Language == "" {
		return Result{Output: "unsupported file type"}, nil
	}
	var out []string
	out = append(out, fmt.Sprintf("path: %s", summary.Path))
	out = append(out, fmt.Sprintf("language: %s", summary.Language))
	if len(summary.Imports) > 0 {
		out = append(out, "imports:")
		for _, item := range summary.Imports {
			out = append(out, "- "+item)
		}
	}
	if len(summary.Symbols) > 0 {
		out = append(out, "symbols:")
		for _, item := range summary.Symbols {
			out = append(out, "- "+item)
		}
	}
	return Result{Output: strings.Join(out, "\n")}, nil
}

type SymbolReader struct{}

func (SymbolReader) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "read_symbol",
		Description: "Read one AST symbol with optional surrounding lines. Cheaper than opening the file.",
		Parameters: objectSchema(map[string]any{
			"path":          stringProperty(workspacePathArgDescription),
			"symbol":        stringProperty("Symbol name from project_map or file_outline."),
			"context_lines": integerProperty("Surrounding lines to include before and after the symbol.", 0),
		}, "path", "symbol"),
	}
}

func (SymbolReader) Run(ctx context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	symbol, err := stringArg(inv.Args, "symbol")
	if err != nil {
		return Result{}, err
	}
	contextLines, err := optionalIntArg(inv.Args, "context_lines", 4)
	if err != nil {
		return Result{}, err
	}
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines > 20 {
		contextLines = 20
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	parseCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start, end, ok, err := findASTSymbolRange(parseCtx, target, filepath.ToSlash(requestedPath), symbol)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Output: "symbol not found"}, nil
	}
	readStart := start - contextLines
	if readStart < 1 {
		readStart = 1
	}
	maxLines := end - readStart + 1 + contextLines
	if maxLines > 160 {
		maxLines = 160
	}
	return FileReader{}.Run(ctx, Invocation{
		CWD: inv.CWD,
		Args: map[string]any{
			"path":       requestedPath,
			"start_line": readStart,
			"max_lines":  maxLines,
		},
	})
}
