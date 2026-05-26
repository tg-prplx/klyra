package router

import "testing"

func TestClassify(t *testing.T) {
	if Classify("осмотри проект") != ClassFast {
		t.Fatal("expected fast classification")
	}
	if Classify("исправ тесты") != ClassEdit {
		t.Fatal("expected edit classification")
	}
	if Classify("security audit architecture") != ClassDeep {
		t.Fatal("expected deep classification")
	}
}

func TestSelectModel(t *testing.T) {
	routes := ModelRoutes{"fast": "cheap", "edit": "coder", "deep": "reasoner"}
	if SelectModel("default", routes, "осмотри проект") != "cheap" {
		t.Fatal("expected fast model")
	}
	if SelectModel("default", routes, "реализуй feature") != "coder" {
		t.Fatal("expected edit model")
	}
	if SelectModel("default", nil, "anything") != "default" {
		t.Fatal("expected default model")
	}
}
