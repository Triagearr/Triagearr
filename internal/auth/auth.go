// Package auth holds the password primitives shared by the two entry points
// that write operator credentials: the HTTP dashboard (internal/server) and the
// recovery CLI (triagearr auth set-password). Keeping generation, the bcrypt
// cost and the minimum-length rule in one place stops the two paths from
// drifting apart on security-relevant constants.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// GeneratedPasswordLength is the number of base64url chars in an
// auto-generated password handed back to the operator exactly once.
const GeneratedPasswordLength = 32

// MinPasswordRunes is the minimum length for an operator-chosen password.
// Short by browser-form standards on purpose: the dashboard is single-user and
// the cookie/X-API-Key flow is rate-limited.
const MinPasswordRunes = 12

// ErrTooShort signals an operator-chosen password below MinPasswordRunes. The
// HTTP handler maps it to a 400; the CLI surfaces it as a usage error.
var ErrTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordRunes)

// Valid reports whether a password meets the minimum-length rule.
func Valid(s string) bool {
	return utf8.RuneCountInString(s) >= MinPasswordRunes
}

// Generate returns a base64url-encoded random password of GeneratedPasswordLength
// characters.
func Generate() (string, error) {
	// 3 bytes → 4 chars of base64url.
	nBytes := (GeneratedPasswordLength*3 + 3) / 4
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(buf)
	if utf8.RuneCountInString(s) > GeneratedPasswordLength {
		s = s[:GeneratedPasswordLength]
	}
	return s, nil
}

// Hash returns the bcrypt hash of a password at the default cost.
func Hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(h), nil
}

// Resolve returns the plaintext password to use (auto-generating one when
// supplied is empty), its bcrypt hash, and whether it was generated. A supplied
// password shorter than MinPasswordRunes returns ErrTooShort.
func Resolve(supplied string) (plain, hash string, generated bool, err error) {
	plain = supplied
	if plain == "" {
		plain, err = Generate()
		if err != nil {
			return "", "", false, fmt.Errorf("generating password: %w", err)
		}
		generated = true
	}
	if !Valid(plain) {
		return "", "", false, ErrTooShort
	}
	hash, err = Hash(plain)
	if err != nil {
		return "", "", false, err
	}
	return plain, hash, generated, nil
}
