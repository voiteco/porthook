// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func FuzzRequestLogListOptions(f *testing.F) {
	f.Add("limit=10&status=200&since=2026-07-10T00%3A00%3A00Z&until=2026-07-10T01%3A00%3A00Z")
	f.Add("cursor=not-a-cursor&status=999")
	f.Add("%zz")

	f.Fuzz(func(t *testing.T, rawQuery string) {
		req := httptest.NewRequest(http.MethodGet, "http://gateway.example.test/api/v1/request-logs", nil)
		req.URL.RawQuery = rawQuery
		_, _ = requestLogListOptionsFromRequest(req, defaultRequestLogLimit)
	})
}
