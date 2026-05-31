package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"klyra/pkg/llm"
)

func TestWebSearchParsesResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "klyra" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><a class="result__a" href="https://example.com/a">Example <b>A</b></a></html>`))
	}))
	defer server.Close()

	result, err := WebSearch{Endpoint: server.URL + "?q=%s", Client: server.Client()}.Run(context.Background(), Invocation{
		Args: map[string]any{"query": "klyra", "max_results": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Example A - https://example.com/a") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestWebSearchRespectsCallerCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := WebSearch{Endpoint: server.URL + "?q=%s", Client: server.Client()}.Run(ctx, Invocation{
		Args: map[string]any{"query": "klyra", "max_results": 3},
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation took too long: %s", elapsed)
	}
}

func TestFetchURLConvertsHTMLToText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><head><style>.x{}</style></head><body><h1>Title</h1><p>Hello &amp; world</p></body></html>`))
	}))
	defer server.Close()

	result, err := FetchURL{Client: server.Client()}.Run(context.Background(), Invocation{
		Args: map[string]any{"url": server.URL, "max_bytes": 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Title") || !strings.Contains(result.Output, "Hello & world") || strings.Contains(result.Output, ".x") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestFetchURLUsesFocusedRetrieval(t *testing.T) {
	page := `<html><body>
		<h1>Release notes</h1>
		<p>` + strings.Repeat("navigation overview unrelated filler ", 120) + `</p>
		<h2>Authentication</h2>
		<p>The new ValidateToken flow checks token validation errors and refresh handling.</p>
		<p>Use the auth middleware when token validation fails.</p>
		<h2>Unrelated</h2>
		<p>` + strings.Repeat("billing invoice export ", 120) + `</p>
	</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	result, err := FetchURL{Client: server.Client()}.Run(context.Background(), Invocation{
		Args: map[string]any{
			"url":        server.URL,
			"max_bytes":  20000,
			"query":      "token validation auth",
			"max_tokens": 400,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "embeddings: local-hash") || !strings.Contains(result.Output, "ValidateToken") || !strings.Contains(result.Output, "token validation") {
		t.Fatalf("focused retrieval missed relevant page chunk:\n%s", result.Output)
	}
	if strings.Count(result.Output, "billing invoice export") > 3 {
		t.Fatalf("focused retrieval included too much unrelated content:\n%s", result.Output)
	}
}

func TestFetchURLFallsBackWhenNoFocusQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><p>General page text</p></body></html>`))
	}))
	defer server.Close()

	result, err := FetchURL{Client: server.Client()}.Run(context.Background(), Invocation{
		Args: map[string]any{"url": server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output, "embeddings: local-hash") || !strings.Contains(result.Output, "General page text") {
		t.Fatalf("expected non-focused fallback output, got:\n%s", result.Output)
	}
}

func TestWebToolsExposedForInternetTasks(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForCapabilities("arbitrary text", "", nil, map[string]bool{CapabilityWeb: true})
	if !hasToolSpec(specs, "web_search") || !hasToolSpec(specs, "fetch_url") {
		t.Fatalf("expected web tools, got %+v", specs)
	}
}

func TestWebToolsHiddenForNonWebTasks(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("что ты умеешь")
	if hasToolSpec(specs, "web_search") || hasToolSpec(specs, "fetch_url") {
		t.Fatalf("non-web task should not pay for web tool schemas: %+v", specs)
	}
}

func hasToolSpec(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}
