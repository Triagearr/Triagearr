package auth

import (
	"errors"
	"testing"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"one below min", "12345678901", false},
		{"exactly min", "123456789012", true},
		{"well above", "a-long-enough-password", true},
		{"multibyte counts runes not bytes", "héhéhéhéhéhé", true}, // 12 runes
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Valid(tt.in); got != tt.want {
				t.Fatalf("Valid(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := utf8.RuneCountInString(a); n != GeneratedPasswordLength {
		t.Fatalf("Generate length = %d, want %d", n, GeneratedPasswordLength)
	}
	if !Valid(a) {
		t.Fatalf("generated password should satisfy Valid, got %q", a)
	}
	b, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a == b {
		t.Fatal("two Generate calls returned the same password")
	}
}

func TestResolve(t *testing.T) {
	t.Run("empty auto-generates a valid, verifiable hash", func(t *testing.T) {
		plain, hash, generated, err := Resolve("")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !generated {
			t.Fatal("generated = false, want true for empty input")
		}
		if !Valid(plain) {
			t.Fatalf("generated plaintext fails Valid: %q", plain)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
			t.Fatalf("hash does not verify against plaintext: %v", err)
		}
	})

	t.Run("too short returns ErrTooShort", func(t *testing.T) {
		_, _, _, err := Resolve("short")
		if !errors.Is(err, ErrTooShort) {
			t.Fatalf("err = %v, want ErrTooShort", err)
		}
	})

	t.Run("valid supplied is hashed, not regenerated", func(t *testing.T) {
		const pw = "correct-horse-battery"
		plain, hash, generated, err := Resolve(pw)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if generated {
			t.Fatal("generated = true, want false for supplied password")
		}
		if plain != pw {
			t.Fatalf("plain = %q, want %q", plain, pw)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)); err != nil {
			t.Fatalf("hash does not verify: %v", err)
		}
	})
}
