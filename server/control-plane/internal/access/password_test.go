// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !IsPasswordHash(hash) {
		t.Fatalf("IsPasswordHash(%q) = false, want true", hash)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want $argon2id$ prefix", hash)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword = false, want true for correct password")
	}

	ok, err = VerifyPassword(hash, "wrong password")
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword = true, want false for incorrect password")
	}
}

func TestHashPasswordProducesDistinctSaltsAndHashes(t *testing.T) {
	first, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	second, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if first == second {
		t.Fatal("HashPassword produced identical output for two calls, want distinct salts")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	if _, err := VerifyPassword("not-a-hash", "password"); err == nil {
		t.Fatal("VerifyPassword returned nil error for malformed hash")
	}
	if _, err := VerifyPassword(HashSecret("password"), "password"); err == nil {
		t.Fatal("VerifyPassword returned nil error for a legacy SHA-256 hash")
	}
}

func TestIsPasswordHashDistinguishesLegacyHashes(t *testing.T) {
	if IsPasswordHash(HashSecret("token")) {
		t.Fatal("IsPasswordHash = true for a legacy SHA-256 hash, want false")
	}
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !IsPasswordHash(hash) {
		t.Fatal("IsPasswordHash = false for an Argon2id hash, want true")
	}
}
