package tools

import (
	"context"
	"strings"
	"testing"
)

func TestDiscoverToolsUnlocksExplicitCapabilities(t *testing.T) {
	result, err := (DiscoverTools{}).Run(context.Background(), Invocation{Args: map[string]any{
		"capabilities": []any{"web", "workspace", "web"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "web, workspace") {
		t.Fatalf("unexpected discovery output: %s", result.Output)
	}
}

func TestDiscoverToolsRejectsUnknownCapability(t *testing.T) {
	_, err := (DiscoverTools{}).Run(context.Background(), Invocation{Args: map[string]any{
		"capabilities": []any{"magic"},
	}})
	if err == nil || !strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("expected capability validation error, got %v", err)
	}
}

func TestURLStructurallyUnlocksWebTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("summarize https://example.com/releases")
	if !hasSpec(specs, "web_search") || !hasSpec(specs, "fetch_url") {
		t.Fatalf("URL should structurally expose web tools: %+v", specs)
	}
}

func TestUnknownFileExtensionStructurallyUnlocksWorkspaceTools(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTaskMode("inspect src/widget.zig", "inspect", nil)
	if !hasSpec(specs, "read_file") {
		t.Fatalf("file path shortcut should not depend on a language allowlist: %+v", specs)
	}
}

func TestEditCapabilityIncludesWorkspaceReads(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForCapabilities("arbitrary text", "edit", nil, map[string]bool{CapabilityEdit: true})
	if !hasSpec(specs, "create_file") || !hasSpec(specs, "project_map") || !hasSpec(specs, "read_file") {
		t.Fatalf("edit capability should include workspace reads: %+v", specs)
	}
}
