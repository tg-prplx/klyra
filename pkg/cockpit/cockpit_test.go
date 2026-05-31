package cockpit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCreatesBudgetedFactCards(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example\n")
	writeFile(t, root, "README.md", "# Example\n")
	writeFile(t, root, "pkg/app/app.go", "package app\n\nfunc Run() error { return nil }\n")
	writeFile(t, root, "AGENTS.md", "Keep changes small.\n")
	writeFile(t, root, ".agentcli/recipes/testing-rules.md", "Run focused tests.")
	writeFile(t, root, "go.sum", "example hash\n")

	snapshot, err := Build(context.Background(), Config{
		Enabled:         true,
		Inject:          true,
		MaxTokens:       600,
		MaxFiles:        10,
		IncludeDiff:     true,
		IncludeRecipes:  true,
		IncludeNegative: true,
	}, root, "test Run", []string{"pkg/app/app_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Enabled || !snapshot.Injected {
		t.Fatalf("expected enabled injected snapshot: %+v", snapshot)
	}
	if snapshot.EstimatedTokens > snapshot.MaxTokens {
		t.Fatalf("snapshot exceeded budget: %d > %d", snapshot.EstimatedTokens, snapshot.MaxTokens)
	}
	text := snapshot.Markdown()
	if strings.Contains(text, "###") || strings.Contains(text, "##") {
		t.Fatalf("cockpit markdown should avoid unsupported headers:\n%s", text)
	}
	for _, want := range []string{"Repo Map", "Context Cart", "Agent Rails", "Context Recipes", "Negative Context"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in cockpit markdown:\n%s", want, text)
		}
	}
	prompt := snapshot.PromptText()
	if strings.Contains(prompt, "```") || strings.Contains(prompt, "freshness") || strings.Contains(prompt, "kind:") {
		t.Fatalf("prompt text should be compact, not UI markdown:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[Repo Map]") || !strings.Contains(prompt, "map/search/outline") {
		t.Fatalf("prompt text should keep card content:\n%s", prompt)
	}
}

func TestBuildDisabledReturnsDisabledSnapshot(t *testing.T) {
	snapshot, err := Build(context.Background(), Config{Enabled: false}, t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Enabled {
		t.Fatalf("expected disabled snapshot: %+v", snapshot)
	}
	if snapshot.Markdown() != "context cockpit disabled" {
		t.Fatalf("unexpected markdown: %q", snapshot.Markdown())
	}
}

func TestNegativeContextDoesNotHideMigrationsByName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "db/migrations/202001010001_create_users.sql", "create table users(id integer);\n")
	if negative := detectNegativeContext(root, 10); strings.Contains(negative, "202001010001_create_users.sql") {
		t.Fatalf("migration should not be withheld by a domain-specific filename heuristic:\n%s", negative)
	}
}

func TestRetrievalIncludesUnknownTextExtensionsButSkipsSecrets(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/widget.zig", "pub fn frobnicate_widget() void {}\n")
	writeFile(t, root, ".env", "FROBNICATE_WIDGET_SECRET=do-not-read\n")

	cart, warnings := buildRetrievalCart(context.Background(), retrievalConfig{
		MaxTokens: 300,
		MaxChunks: 3,
		MaxFiles:  10,
	}, root, "frobnicate_widget")
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if !strings.Contains(cart, "src/widget.zig") {
		t.Fatalf("unknown text extension should be retrievable:\n%s", cart)
	}
	if strings.Contains(cart, "do-not-read") || strings.Contains(cart, ".env") {
		t.Fatalf("secret files must stay outside retrieval:\n%s", cart)
	}
}

func TestBuildIncludesRetrievalCartWithBudgetAndDenyList(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/auth/login.go", `package auth

func ValidateToken(raw string) bool {
	return raw == "valid"
}
`)
	writeFile(t, root, "pkg/auth/login_test.go", `package auth

func TestValidateToken(t *testing.T) {
	if !ValidateToken("valid") {
		t.Fatal("token rejected")
	}
}
`)
	writeFile(t, root, "vendor/auth/ignored.go", "package ignored\nfunc ValidateToken() {}\n")
	writeFile(t, root, "pkg/auth/client.generated.go", "package auth\nfunc ValidateTokenGenerated() {}\n")
	writeFile(t, root, "package-lock.json", `{"ignored": true}`)

	snapshot, err := Build(context.Background(), Config{
		Enabled:          true,
		Inject:           true,
		MaxTokens:        1000,
		MaxFiles:         20,
		MaxCards:         4,
		IncludeRetrieval: true,
		RetrievalTokens:  450,
		RetrievalChunks:  3,
	}, root, "ValidateToken auth failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := snapshot.Markdown()
	if !strings.Contains(text, "Retrieval Cart") || !strings.Contains(text, "pkg/auth/login.go") {
		t.Fatalf("expected retrieval cart with auth chunk:\n%s", text)
	}
	if !strings.Contains(text, "tokens=") || !strings.Contains(text, "why:") {
		t.Fatalf("expected selected chunks to show token prices and reasons:\n%s", text)
	}
	for _, blocked := range []string{"vendor/auth/ignored.go", "client.generated.go", "package-lock.json"} {
		if strings.Contains(text, blocked) {
			t.Fatalf("expected deny-listed path %q to be withheld:\n%s", blocked, text)
		}
	}
	if len(snapshot.Cards) > 4 {
		t.Fatalf("expected max card budget to hold: %+v", snapshot.Cards)
	}
	if snapshot.EstimatedTokens > snapshot.MaxTokens {
		t.Fatalf("snapshot exceeded budget: %d > %d", snapshot.EstimatedTokens, snapshot.MaxTokens)
	}
}

func TestRetrievalCartSkipsChunksOverBudget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/auth/large.go", "package auth\n\nfunc ValidateToken() bool {\n"+strings.Repeat("\t_ = \"ValidateToken budget overflow\"\n", 80)+"\treturn true\n}\n")

	cart, warnings := buildRetrievalCart(context.Background(), retrievalConfig{
		MaxTokens: 5,
		MaxChunks: 3,
		MaxFiles:  10,
	}, root, "ValidateToken")
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if strings.TrimSpace(cart) != "" {
		t.Fatalf("expected over-budget chunk to be skipped, got:\n%s", cart)
	}
}

func TestRetrievalCartUsesLocalEmbeddingsForIdentifierSubtokens(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/security/check.go", `package security

func ValidateToken(raw string) bool {
	return raw != ""
}
`)
	writeFile(t, root, "pkg/other/cache.go", `package other

func StoreValue(raw string) bool {
	return raw != ""
}
`)

	withoutEmbeddings, _ := buildRetrievalCart(context.Background(), retrievalConfig{
		MaxTokens:     300,
		MaxChunks:     2,
		MaxFiles:      10,
		UseEmbeddings: false,
	}, root, "token validation")
	if strings.Contains(withoutEmbeddings, "pkg/security/check.go") {
		t.Fatalf("expected lexical-only retrieval to miss camel-case semantic match:\n%s", withoutEmbeddings)
	}

	withEmbeddings, warnings := buildRetrievalCart(context.Background(), retrievalConfig{
		MaxTokens:     300,
		MaxChunks:     2,
		MaxFiles:      10,
		UseEmbeddings: true,
	}, root, "token validation")
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if !strings.Contains(withEmbeddings, "pkg/security/check.go") || !strings.Contains(withEmbeddings, "local-embedding") {
		t.Fatalf("expected local embeddings to retrieve identifier subtoken match:\n%s", withEmbeddings)
	}
	if strings.Contains(withEmbeddings, "configured on, but MVP") {
		t.Fatalf("embedding retrieval should be real, not a placeholder note:\n%s", withEmbeddings)
	}
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
