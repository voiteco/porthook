// SPDX-License-Identifier: Apache-2.0

package names

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

const (
	MaxSubdomainLength = 63
	randomAlphabet     = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func ValidateSubdomain(name string) error {
	if name == "" {
		return errors.New("subdomain is required")
	}
	if len(name) > MaxSubdomainLength {
		return fmt.Errorf("subdomain must be at most %d characters", MaxSubdomainLength)
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return errors.New("subdomain cannot start or end with hyphen")
	}

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return errors.New("subdomain can contain only lowercase ASCII letters, numbers, and hyphens")
		}
	}

	return nil
}

func RandomSubdomain(length int) (string, error) {
	if length <= 0 || length > MaxSubdomainLength {
		return "", fmt.Errorf("random subdomain length must be between 1 and %d", MaxSubdomainLength)
	}

	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(randomAlphabet))))
		if err != nil {
			return "", fmt.Errorf("generate random subdomain: %w", err)
		}
		out[i] = randomAlphabet[n.Int64()]
	}
	return string(out), nil
}
