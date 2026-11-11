package slug

import (
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	tests := map[string]string{
		"Hello, World!":         "hello-world",
		"  Multiple   Spaces ":  "multiple-spaces",
		"Charity_Gala-2026":     "charity-gala-2026",
		"a--b__c":               "a-b-c",
		"!!!":                   "item",
		"":                      "item",
		"Live & Loud / Jakarta": "live-loud-jakarta",
	}
	for input, want := range tests {
		if got := Make(input); got != want {
			t.Errorf("Make(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMakeTruncates(t *testing.T) {
	got := Make(strings.Repeat("word-", 40))
	if len(got) > 120 {
		t.Fatalf("slug length %d exceeds 120", len(got))
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Fatalf("truncated slug %q has leading or trailing dash", got)
	}
}
