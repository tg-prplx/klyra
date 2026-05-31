package router

import "testing"

func TestClassify(t *testing.T) {
	if Classify("inspect") != ClassFast {
		t.Fatal("expected fast classification")
	}
	if Classify("edit") != ClassEdit {
		t.Fatal("expected edit classification")
	}
	if Classify("plan") != ClassDeep || Classify("refactor") != ClassDeep {
		t.Fatal("expected deep classification")
	}
}

func TestSelectModel(t *testing.T) {
	routes := ModelRoutes{"fast": "cheap", "edit": "coder", "deep": "reasoner"}
	if SelectModel("default", routes, "inspect") != "cheap" {
		t.Fatal("expected fast model")
	}
	if SelectModel("default", routes, "edit") != "coder" {
		t.Fatal("expected edit model")
	}
	if SelectModel("default", nil, "anything") != "default" {
		t.Fatal("expected default model")
	}
}
