// Package password centralises hashing and password policy.
package password

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const (
	MinLength = 8
	MaxLength = 72 // bcrypt input limit in bytes
	cost      = 12
)

var (
	ErrTooShort = errors.New("password must be at least 8 characters")
	ErrTooLong  = errors.New("password must be at most 72 characters")
	ErrTooWeak  = errors.New("password must contain at least one letter and one digit")
)

// Validate enforces the password policy.
func Validate(plain string) error {
	if len(plain) < MinLength {
		return ErrTooShort
	}
	if len(plain) > MaxLength {
		return ErrTooLong
	}
	var hasLetter, hasDigit bool
	for _, r := range plain {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrTooWeak
	}
	return nil
}

func Hash(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

var dummyHash = sync.OnceValues(func() (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("fayrly-timing-equaliser-0000"), cost)
	return string(hash), err
})

// VerifyDummy burns roughly the same time as a real verification to reduce
// account-enumeration timing signals.
func VerifyDummy(plain string) {
	hash, err := dummyHash()
	if err == nil {
		_ = bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	}
}

const (
	upperChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lowerChars  = "abcdefghijkmnopqrstuvwxyz"
	digitChars  = "23456789"
	symbolChars = "!@#$%^&*"
)

// Generate returns a random password that satisfies the policy.
func Generate() (string, error) {
	const length = 16
	alphabet := upperChars + lowerChars + digitChars + symbolChars

	out := make([]byte, 0, length)
	for _, set := range []string{upperChars, lowerChars, digitChars, symbolChars} {
		ch, err := pick(set)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}
	for len(out) < length {
		ch, err := pick(alphabet)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}

func pick(set string) (byte, error) {
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
	if err != nil {
		return 0, err
	}
	return set[idx.Int64()], nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
