package password

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"valid", "GoodPass1", nil},
		{"valid with symbol", "Fayrly!2026", nil},
		{"too short", "Pass1", ErrTooShort},
		{"too long", strings.Repeat("a1", 40), ErrTooLong},
		{"letters only", "OnlyLetters", ErrTooWeak},
		{"digits only", "123456789", ErrTooWeak},
		{"empty", "", ErrTooShort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate(%q) = %v, want %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestHashAndVerify(t *testing.T) {
	hash, err := Hash("Secret123!")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash == "Secret123!" {
		t.Fatal("hash must not equal the plaintext")
	}
	if !Verify(hash, "Secret123!") {
		t.Fatal("correct password should verify")
	}
	if Verify(hash, "Wrong123!") {
		t.Fatal("wrong password must not verify")
	}
}

func TestGenerateSatisfiesPolicy(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := Validate(first); err != nil {
		t.Fatalf("generated password fails policy: %v", err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if first == second {
		t.Fatal("two generated passwords must differ")
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Admin@Example.COM "); got != "admin@example.com" {
		t.Fatalf("NormalizeEmail = %q", got)
	}
}
