package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestPasswordBounds(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "", false}, {"short", strings.Repeat("a", 11), false},
		{"minimum", strings.Repeat("a", 12), true}, {"maximum", strings.Repeat("a", 128), true},
		{"too long", strings.Repeat("a", 129), false},
		{"unicode minimum", strings.Repeat("🦊", 12), true},
		{"unicode maximum", strings.Repeat("🦊", 128), true},
		{"unicode too short", strings.Repeat("🦊", 11), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.value)
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid = %v", err, tc.valid)
			}
			if !tc.valid {
				if _, err := HashPassword(tc.value); !errors.Is(err, ErrInvalidPassword) {
					t.Fatal("invalid password was hashed")
				}
			}
		})
	}
}

func TestSaltedPasswordAndMalformedHashes(t *testing.T) {
	password := "test-password-🦊"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes reuse the salt")
	}
	if !VerifyPassword(first, password) || !VerifyPassword(second, password) || VerifyPassword(first, password+"wrong") {
		t.Fatal("password verification")
	}
	parts := strings.Split(first, "$")
	for _, tc := range []struct {
		name  string
		field int
		value string
	}{
		{"algorithm", 1, "argon2i"}, {"version", 2, "v=16"},
		{"low memory", 3, "m=1,t=2,p=1"}, {"high memory", 3, "m=262145,t=2,p=1"},
		{"iterations", 3, "m=19456,t=11,p=1"}, {"parallelism", 3, "m=19456,t=2,p=0"},
		{"trailing parameters", 3, "m=19456,t=2,p=1,unknown=1"},
		{"short salt", 4, "YQ"}, {"invalid salt", 4, "!"}, {"short hash", 5, "YQ"}, {"invalid hash", 5, "!"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			malformed := append([]string(nil), parts...)
			malformed[tc.field] = tc.value
			if VerifyPassword(strings.Join(malformed, "$"), password) {
				t.Fatal("malformed hash accepted")
			}
		})
	}
	for _, malformed := range []string{"", "argon2", first + "$extra"} {
		if VerifyPassword(malformed, password) {
			t.Fatal("invalid encoding accepted")
		}
	}
}
