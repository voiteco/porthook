// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters follow the OWASP password storage cheat sheet
// baseline (m=19 MiB, t=2, p=1) for a single-tenant control-plane process.
const (
	argon2idPrefix     = "$argon2id$"
	argon2idMemoryKiB  = 19 * 1024
	argon2idIterations = 2
	argon2idParallel   = 1
	argon2idSaltBytes  = 16
	argon2idKeyBytes   = 32
)

var ErrPasswordHashMalformed = errors.New("malformed argon2id password hash")

// HashPassword derives a versioned, self-describing Argon2id hash for a
// user-selected basic-auth password. Unlike HashSecret, callers must not use
// this for generated high-entropy tokens: the memory-hard KDF cost is
// unnecessary and slower for values that are already infeasible to guess.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2idSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argon2idIterations, argon2idMemoryKiB, argon2idParallel, argon2idKeyBytes)
	return fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2idPrefix,
		argon2.Version,
		argon2idMemoryKiB,
		argon2idIterations,
		argon2idParallel,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// IsPasswordHash reports whether hash was produced by HashPassword, as
// opposed to the legacy unsalted SHA-256 hex hash HashSecret produces.
func IsPasswordHash(hash string) bool {
	return strings.HasPrefix(hash, argon2idPrefix)
}

// VerifyPassword performs a constant-time comparison of password against an
// Argon2id hash produced by HashPassword.
func VerifyPassword(hash, password string) (bool, error) {
	memoryKiB, iterations, parallel, salt, key, err := parseArgon2idHash(hash)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, iterations, memoryKiB, parallel, uint32(len(key)))
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

func parseArgon2idHash(hash string) (memoryKiB, iterations uint32, parallel uint8, salt, key []byte, err error) {
	if !strings.HasPrefix(hash, argon2idPrefix) {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}
	fields := strings.Split(strings.TrimPrefix(hash, argon2idPrefix), "$")
	if len(fields) != 4 {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}

	var version int
	if _, err := fmt.Sscanf(fields[0], "v=%d", &version); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: %v", ErrPasswordHashMalformed, err)
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: unsupported version %d", ErrPasswordHashMalformed, version)
	}

	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(fields[1], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: %v", ErrPasswordHashMalformed, err)
	}

	salt, err = base64.RawStdEncoding.DecodeString(fields[2])
	if err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: %v", ErrPasswordHashMalformed, err)
	}
	key, err = base64.RawStdEncoding.DecodeString(fields[3])
	if err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: %v", ErrPasswordHashMalformed, err)
	}
	if len(salt) == 0 || len(key) == 0 {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}

	return m, t, p, salt, key, nil
}
