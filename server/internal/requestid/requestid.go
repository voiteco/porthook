// SPDX-License-Identifier: AGPL-3.0-only

package requestid

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

const Header = "X-Request-ID"

type contextKey struct{}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := inbound(r)
		if id == "" {
			id = New()
		}
		w.Header().Set(Header, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}

func FromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id := FromContext(r.Context()); id != "" {
		return id
	}
	return inbound(r)
}

func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

func New() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return ""
	}
	return "req_" + base64.RawURLEncoding.EncodeToString(data)
}

func inbound(r *http.Request) string {
	for _, header := range []string{Header, "X-Correlation-ID"} {
		if id := clean(r.Header.Get(header)); id != "" {
			return id
		}
	}
	return ""
}

func clean(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return ""
		}
	}
	return value
}
