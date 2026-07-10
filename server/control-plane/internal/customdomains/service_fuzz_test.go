// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import "testing"

func FuzzNormalizeHostname(f *testing.F) {
	f.Add(" Preview.Example.TEST. ")
	f.Add("bad_name.example.test")
	f.Add("*.example.test")

	f.Fuzz(func(t *testing.T, hostname string) {
		_, _ = NormalizeHostname(hostname)
		_ = VerificationName(hostname)
	})
}
