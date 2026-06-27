// SPDX-License-Identifier: Apache-2.0

package names

import "testing"

func TestValidateSubdomainAcceptsValidNames(t *testing.T) {
	for _, name := range []string{"demo", "demo-1", "a", "abc123"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSubdomain(name); err != nil {
				t.Fatalf("ValidateSubdomain returned error: %v", err)
			}
		})
	}
}

func TestValidateSubdomainRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "-demo", "demo-", "Demo", "demo_test", "demo.test"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSubdomain(name); err == nil {
				t.Fatal("ValidateSubdomain returned nil error")
			}
		})
	}
}

func TestRandomSubdomain(t *testing.T) {
	name, err := RandomSubdomain(12)
	if err != nil {
		t.Fatalf("RandomSubdomain returned error: %v", err)
	}
	if len(name) != 12 {
		t.Fatalf("len(name) = %d, want 12", len(name))
	}
	if err := ValidateSubdomain(name); err != nil {
		t.Fatalf("generated invalid subdomain: %v", err)
	}
}
