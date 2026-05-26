package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"agentcli/pkg/llm"
)

type GoSymbolReader struct{}

func (GoSymbolReader) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "read_go_symbol",
		Description: "Read a specific Go function, method, type, var, or const declaration by name without loading the whole file.",
		Parameters: objectSchema(map[string]any{
			"path":   stringProperty("Relative Go file path."),
			"symbol": stringProperty("Symbol name. Methods can be Type.Method or Method."),
		}, "path", "symbol"),
	}
}

func (GoSymbolReader) Run(_ context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	symbol, err := stringArg(inv.Args, "symbol")
	if err != nil {
		return Result{}, err
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return Result{}, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, target, data, parser.ParseComments)
	if err != nil {
		return Result{}, err
	}
	decl := findGoDecl(file, symbol)
	if decl == nil {
		return Result{Output: "symbol not found"}, nil
	}
	start := fset.Position(decl.Pos()).Line
	end := fset.Position(decl.End()).Line
	return FileReader{}.Run(context.Background(), Invocation{
		CWD: inv.CWD,
		Args: map[string]any{
			"path":       requestedPath,
			"start_line": start,
			"max_lines":  end - start + 1,
		},
	})
}

func findGoDecl(file *ast.File, symbol string) ast.Node {
	symbol = strings.TrimSpace(symbol)
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if goFuncName(typed) == symbol || typed.Name.Name == symbol {
				return typed
			}
		case *ast.GenDecl:
			if genDeclMatches(typed, symbol) {
				return typed
			}
		}
	}
	return nil
}

func goFuncName(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}
	receiver := exprName(decl.Recv.List[0].Type)
	if receiver == "" {
		return decl.Name.Name
	}
	return fmt.Sprintf("%s.%s", strings.TrimPrefix(receiver, "*"), decl.Name.Name)
}

func exprName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return exprName(typed.X)
	case *ast.IndexExpr:
		return exprName(typed.X)
	case *ast.IndexListExpr:
		return exprName(typed.X)
	default:
		return ""
	}
}

func genDeclMatches(decl *ast.GenDecl, symbol string) bool {
	for _, spec := range decl.Specs {
		switch typed := spec.(type) {
		case *ast.TypeSpec:
			if typed.Name.Name == symbol {
				return true
			}
		case *ast.ValueSpec:
			for _, name := range typed.Names {
				if name.Name == symbol {
					return true
				}
			}
		}
	}
	return false
}
