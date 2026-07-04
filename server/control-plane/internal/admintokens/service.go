// SPDX-License-Identifier: AGPL-3.0-only

package admintokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var (
	ErrTokenNameRequired = errors.New("admin token name is required")
	ErrUnsupportedScope  = errors.New("unsupported admin token scope")
)

var knownScopes = []string{
	ScopeAdminTokens,
	ScopeTokens,
	ScopeReservations,
	ScopeDomains,
	ScopeAccessPolicies,
	ScopeAuditHistory,
	ScopeRuntimeDiagnostics,
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func KnownScopes() []string {
	return cloneScopes(knownScopes)
}

func DefaultScopes() []string {
	return KnownScopes()
}

func (s *Service) Ready(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("admin token store is required")
	}
	return s.store.Ping(ctx)
}

func (s *Service) CreateToken(ctx context.Context, req CreateTokenRequest) (CreatedToken, error) {
	if s == nil || s.store == nil {
		return CreatedToken{}, errors.New("admin token store is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return CreatedToken{}, ErrTokenNameRequired
	}

	scopes, err := normalizeScopes(req.Scopes)
	if err != nil {
		return CreatedToken{}, err
	}
	if len(scopes) == 0 {
		scopes = DefaultScopes()
	}

	token, err := randomSecret("pat_", 32)
	if err != nil {
		return CreatedToken{}, err
	}
	id, err := randomSecret("adm_", 16)
	if err != nil {
		return CreatedToken{}, err
	}
	createdAt := s.now()
	record := TokenRecord{
		ID:        id,
		Name:      name,
		TokenHash: HashToken(token),
		Scopes:    scopes,
		CreatedAt: createdAt,
	}
	if err := s.store.Create(ctx, record); err != nil {
		return CreatedToken{}, err
	}

	return CreatedToken{
		ID:        id,
		Name:      name,
		Token:     token,
		Scopes:    cloneScopes(scopes),
		CreatedAt: createdAt,
	}, nil
}

func (s *Service) ValidateToken(ctx context.Context, token string, requiredScopes ...string) (ValidationResult, error) {
	if s == nil || s.store == nil {
		return ValidationResult{}, errors.New("admin token store is required")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ValidationResult{Valid: false}, nil
	}
	for _, scope := range requiredScopes {
		scope = strings.TrimSpace(scope)
		if scope != "" && !isKnownScope(scope) {
			return ValidationResult{}, fmt.Errorf("%w %q", ErrUnsupportedScope, scope)
		}
	}

	record, ok, err := s.store.LookupByHash(ctx, HashToken(token))
	if err != nil {
		return ValidationResult{}, err
	}
	if !ok || record.RevokedAt != nil {
		return ValidationResult{Valid: false}, nil
	}
	for _, scope := range requiredScopes {
		if scope == "" {
			continue
		}
		if !hasScope(record.Scopes, scope) {
			return ValidationResult{Valid: false}, nil
		}
	}
	if err := s.store.MarkUsed(ctx, record.ID, s.now()); err != nil {
		return ValidationResult{}, err
	}
	return ValidationResult{
		Valid:   true,
		TokenID: record.ID,
		Name:    record.Name,
		Scopes:  cloneScopes(record.Scopes),
	}, nil
}

func (s *Service) ListTokens(ctx context.Context) (ListTokensResponse, error) {
	if s == nil || s.store == nil {
		return ListTokensResponse{}, errors.New("admin token store is required")
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return ListTokensResponse{}, err
	}
	out := make([]TokenSummary, 0, len(records))
	for _, record := range records {
		out = append(out, summaryFromRecord(record))
	}
	return ListTokensResponse{Tokens: out}, nil
}

func (s *Service) GetToken(ctx context.Context, id string) (TokenSummary, bool, error) {
	if s == nil || s.store == nil {
		return TokenSummary{}, false, errors.New("admin token store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return TokenSummary{}, false, nil
	}
	record, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return TokenSummary{}, false, err
	}
	if !ok {
		return TokenSummary{}, false, nil
	}
	return summaryFromRecord(record), true, nil
}

func (s *Service) RevokeToken(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return errors.New("admin token store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return s.store.Revoke(ctx, id, s.now())
}

func summaryFromRecord(record TokenRecord) TokenSummary {
	return TokenSummary{
		ID:         record.ID,
		Name:       record.Name,
		Scopes:     cloneScopes(record.Scopes),
		CreatedAt:  record.CreatedAt,
		LastUsedAt: cloneTimePtr(record.LastUsedAt),
		RevokedAt:  cloneTimePtr(record.RevokedAt),
	}
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomSecret(prefix string, bytesLen int) (string, error) {
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate admin token secret: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func normalizeScopes(scopes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if !isKnownScope(scope) {
			return nil, fmt.Errorf("%w %q", ErrUnsupportedScope, scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out, nil
}

func isKnownScope(scope string) bool {
	return slices.Contains(knownScopes, scope)
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
