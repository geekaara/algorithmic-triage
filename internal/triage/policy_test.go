package triage

import (
	"os"
	"strings"
	"testing"
)

func TestParsePolicy(t *testing.T) {
	md, err := os.ReadFile("../../context/mvp.md")
	if err != nil {
		t.Fatal(err)
	}
	p := ParsePolicy(string(md))
	if len(p.Steps) != 5 {
		t.Fatalf("want 5 golden-path steps, got %d: %q", len(p.Steps), p.Steps)
	}
	if !strings.Contains(p.Lines["out-of-scope-2"], "AI / ML") {
		t.Errorf("out-of-scope-2 = %q, want the AI / ML line", p.Lines["out-of-scope-2"])
	}
	if !strings.HasPrefix(p.Lines["rule-4"], "Priority labels are inputs") {
		t.Errorf("rule-4 = %q", p.Lines["rule-4"])
	}
	if got := p.Describe("must-2"); !strings.HasPrefix(got, "Non-negotiable: Security and legal") {
		t.Errorf("Describe(must-2) = %q", got)
	}
	for _, l := range p.Order {
		t.Logf("%-15s %s", l, p.Lines[l])
	}
}
