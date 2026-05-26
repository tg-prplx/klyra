package session

import (
	"testing"

	"agentcli/pkg/llm"
)

func TestStoreSaveLoadAndList(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.LoadOrCreate("test/session", ".")
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "test-session" {
		t.Fatalf("expected sanitized id, got %q", session.ID)
	}
	session.Messages = []llm.Message{{Role: llm.RoleUser, Content: "hello"}}
	if err := store.Save(session); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("test-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello" {
		t.Fatalf("unexpected loaded session: %+v", loaded)
	}
	sessions, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
}
