package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/dockerfile"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/sql"
	"github.com/smacker/go-tree-sitter/svelte"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/toml"
	tsxgrammar "github.com/smacker/go-tree-sitter/typescript/tsx"
	tsgrammar "github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/yaml"
)

type astFileSummary struct {
	Path     string
	Language string
	Imports  []string
	Symbols  []string
	Score    int
}

func astSymbolSummaries(ctx context.Context, root string, files []string, focus string) []astFileSummary {
	focusTerms := queryTerms(focus)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var summaries []astFileSummary
	for _, rel := range files {
		summary, err := parseASTFileSummary(ctx, filepath.Join(root, rel), rel, focusTerms)
		if err != nil || len(summary.Symbols) == 0 {
			continue
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Score == summaries[j].Score {
			return summaries[i].Path < summaries[j].Path
		}
		return summaries[i].Score > summaries[j].Score
	})
	if len(summaries) > 32 {
		summaries = summaries[:32]
	}
	return summaries
}

func findASTSymbolRange(ctx context.Context, path, rel, symbol string) (int, int, bool, error) {
	lang, _ := treeSitterLanguageForPath(rel)
	if lang == nil {
		return 0, 0, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false, err
	}
	root, err := sitter.ParseCtx(ctx, data, lang)
	if err != nil || root == nil {
		return 0, 0, false, err
	}
	node := findSymbolNode(root, data, strings.TrimSpace(symbol), 0)
	if node == nil {
		return 0, 0, false, nil
	}
	start := int(node.StartPoint().Row) + 1
	end := int(node.EndPoint().Row) + 1
	if end < start {
		end = start
	}
	return start, end, true, nil
}

func parseASTFileSummary(ctx context.Context, path, rel string, focusTerms []string) (astFileSummary, error) {
	lang, name := treeSitterLanguageForPath(rel)
	if lang == nil {
		return astFileSummary{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return astFileSummary{}, err
	}
	root, err := sitter.ParseCtx(ctx, data, lang)
	if err != nil || root == nil {
		return astFileSummary{}, err
	}
	summary := astFileSummary{Path: filepath.ToSlash(rel), Language: name}
	walkAST(root, data, 0, &summary)
	if len(summary.Imports) > 6 {
		summary.Imports = summary.Imports[:6]
	}
	if len(summary.Symbols) > 12 {
		summary.Symbols = summary.Symbols[:12]
	}
	lower := strings.ToLower(summary.Path + " " + strings.Join(summary.Symbols, " ") + " " + strings.Join(summary.Imports, " "))
	for _, term := range focusTerms {
		if strings.Contains(lower, term) {
			summary.Score += 10
		}
	}
	return summary, nil
}

func findSymbolNode(node *sitter.Node, source []byte, symbol string, depth int) *sitter.Node {
	if node == nil || depth > 10 {
		return nil
	}
	if isSymbolNode(node.Type()) {
		name := nodeName(node, source)
		if symbolNameMatches(name, symbol) {
			return node
		}
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if found := findSymbolNode(node.NamedChild(i), source, symbol, depth+1); found != nil {
			return found
		}
	}
	return nil
}

func symbolNameMatches(name, symbol string) bool {
	name = strings.TrimSpace(name)
	symbol = strings.TrimSpace(symbol)
	if name == "" || symbol == "" {
		return false
	}
	if name == symbol {
		return true
	}
	return strings.HasSuffix(symbol, "."+name) || strings.HasSuffix(name, "."+symbol)
}

func walkAST(node *sitter.Node, source []byte, depth int, summary *astFileSummary) {
	if node == nil || depth > 8 {
		return
	}
	nodeType := node.Type()
	if isImportNode(nodeType) {
		addUnique(&summary.Imports, oneLine(node.Content(source)), 6)
	}
	if isSymbolNode(nodeType) {
		if symbol := symbolLine(node, source); symbol != "" {
			addUnique(&summary.Symbols, symbol, 12)
			summary.Score += symbolScore(nodeType, symbol)
		}
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walkAST(node.NamedChild(i), source, depth+1, summary)
	}
}

func symbolLine(node *sitter.Node, source []byte) string {
	name := nodeName(node, source)
	if name == "" {
		return ""
	}
	kind := symbolKind(node.Type())
	line := int(node.StartPoint().Row) + 1
	return kind + " " + name + " line=" + strconvItoa(line)
}

func nodeName(node *sitter.Node, source []byte) string {
	for _, field := range []string{"name", "declarator", "declaration", "left", "path"} {
		if child := node.ChildByFieldName(field); child != nil && !child.IsNull() {
			if name := firstIdentifier(child, source, 0); name != "" {
				return name
			}
		}
	}
	return firstIdentifier(node, source, 0)
}

func firstIdentifier(node *sitter.Node, source []byte, depth int) string {
	if node == nil || depth > 5 {
		return ""
	}
	switch node.Type() {
	case "identifier", "type_identifier", "property_identifier", "field_identifier", "constant", "simple_identifier", "name":
		return cleanIdentifier(node.Content(source))
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if name := firstIdentifier(node.NamedChild(i), source, depth+1); name != "" {
			return name
		}
	}
	return ""
}

func isImportNode(nodeType string) bool {
	switch nodeType {
	case "import_declaration", "import_statement", "import_from_statement", "use_declaration", "using_directive", "include_directive", "package_clause", "require":
		return true
	default:
		return strings.Contains(nodeType, "import")
	}
}

func isSymbolNode(nodeType string) bool {
	switch nodeType {
	case "function_declaration", "method_declaration", "function_definition", "function_item", "function",
		"method_definition", "class_declaration", "class_definition", "interface_declaration", "interface_item",
		"struct_item", "struct_declaration", "enum_item", "enum_declaration", "trait_item", "impl_item",
		"type_declaration", "type_alias_declaration", "type_item", "module", "mod_item",
		"lexical_declaration", "variable_declaration", "const_declaration":
		return true
	default:
		return strings.Contains(nodeType, "function") ||
			strings.Contains(nodeType, "method") ||
			strings.Contains(nodeType, "class") ||
			strings.Contains(nodeType, "interface") ||
			strings.Contains(nodeType, "struct") ||
			strings.Contains(nodeType, "enum")
	}
}

func symbolKind(nodeType string) string {
	lower := strings.ToLower(nodeType)
	switch {
	case strings.Contains(lower, "class"):
		return "class"
	case strings.Contains(lower, "interface"):
		return "interface"
	case strings.Contains(lower, "struct"):
		return "struct"
	case strings.Contains(lower, "enum"):
		return "enum"
	case strings.Contains(lower, "trait"):
		return "trait"
	case strings.Contains(lower, "type"):
		return "type"
	case strings.Contains(lower, "const"):
		return "const"
	case strings.Contains(lower, "variable"), strings.Contains(lower, "lexical"):
		return "var"
	case strings.Contains(lower, "method"):
		return "method"
	default:
		return "func"
	}
}

func symbolScore(nodeType, symbol string) int {
	score := 1
	if strings.Contains(nodeType, "class") || strings.Contains(nodeType, "interface") || strings.Contains(nodeType, "struct") {
		score += 4
	}
	return score
}

func (s astFileSummary) lines() []string {
	header := "- " + s.Path + " language=" + s.Language
	if len(s.Imports) > 0 {
		header += " imports=" + strings.Join(s.Imports, " | ")
	}
	lines := []string{header}
	for _, symbol := range s.Symbols {
		lines = append(lines, "  - "+symbol)
	}
	return lines
}

func treeSitterLanguageForPath(path string) (*sitter.Language, string) {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	switch ext {
	case ".go":
		return golang.GetLanguage(), "go"
	case ".js", ".mjs", ".cjs":
		return javascript.GetLanguage(), "javascript"
	case ".ts":
		return tsgrammar.GetLanguage(), "typescript"
	case ".tsx":
		return tsxgrammar.GetLanguage(), "tsx"
	case ".jsx":
		return javascript.GetLanguage(), "jsx"
	case ".py":
		return python.GetLanguage(), "python"
	case ".rs":
		return rust.GetLanguage(), "rust"
	case ".java":
		return java.GetLanguage(), "java"
	case ".c", ".h":
		return c.GetLanguage(), "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh":
		return cpp.GetLanguage(), "cpp"
	case ".rb":
		return ruby.GetLanguage(), "ruby"
	case ".php":
		return php.GetLanguage(), "php"
	case ".cs":
		return csharp.GetLanguage(), "csharp"
	case ".kt", ".kts":
		return kotlin.GetLanguage(), "kotlin"
	case ".swift":
		return swift.GetLanguage(), "swift"
	case ".lua":
		return lua.GetLanguage(), "lua"
	case ".sh", ".bash", ".zsh":
		return bash.GetLanguage(), "bash"
	case ".sql":
		return sql.GetLanguage(), "sql"
	case ".html", ".htm":
		return html.GetLanguage(), "html"
	case ".css", ".scss", ".sass":
		return css.GetLanguage(), "css"
	case ".svelte":
		return svelte.GetLanguage(), "svelte"
	case ".yaml", ".yml":
		return yaml.GetLanguage(), "yaml"
	case ".toml":
		return toml.GetLanguage(), "toml"
	}
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
		return dockerfile.GetLanguage(), "dockerfile"
	}
	return nil, ""
}

var identCleaner = regexp.MustCompile(`[^A-Za-z0-9_.$:#<>-]+`)

func cleanIdentifier(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "`'\"")
	value = identCleaner.ReplaceAllString(value, "")
	return value
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 120 {
		value = value[:117] + "..."
	}
	return value
}

func addUnique(items *[]string, value string, limit int) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, item := range *items {
		if item == value {
			return
		}
	}
	if limit > 0 && len(*items) >= limit {
		return
	}
	*items = append(*items, value)
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
