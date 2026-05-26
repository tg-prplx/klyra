package tools

import (
	"strings"
	"testing"
)

func TestCompressOutputKeepsImportantLines(t *testing.T) {
	var input strings.Builder
	input.WriteString("\x1b[31mstarting\x1b[0m\n")
	for i := 0; i < 40; i++ {
		input.WriteString("progress line\n")
	}
	input.WriteString("ERROR: test failed\n")
	for i := 0; i < 40; i++ {
		input.WriteString("more progress\n")
	}

	got := CompressOutput(input.String(), 12)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("expected ANSI escape sequences to be stripped: %q", got)
	}
	if !strings.Contains(got, "ERROR: test failed") {
		t.Fatalf("expected important error line to be retained: %q", got)
	}
	if !strings.Contains(got, "output compressed") {
		t.Fatalf("expected compression marker: %q", got)
	}
}
