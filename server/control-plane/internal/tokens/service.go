// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrTokenNameRequired = errors.New("token name is required")

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

func (s *Service) CreateToken(ctx context.Context, req CreateTokenRequest) (CreatedToken, error) {
	if s == nil || s.store == nil {
		return CreatedToken{}, errors.New("token store is required")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return CreatedToken{}, ErrTokenNameRequired
	}

	token, err := randomSecret("ph_", 32)
	if err != nil {
		return CreatedToken{}, err
	}
	id, err := randomSecret("tok_", 16)
	if err != nil {
		return CreatedToken{}, err
	}

	scopes := normalizeScopes(req.Scopes)
	if len(scopes) == 0 {
		scopes = []string{ScopeRegisterTunnel}
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

func (s *Service) ValidateToken(ctx context.Context, token, requiredScope string) (ValidationResult, error) {
	if s == nil || s.store == nil {
		return ValidationResult{}, errors.New("token store is required")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ValidationResult{Valid: false}, nil
	}

	record, ok, err := s.store.LookupByHash(ctx, HashToken(token))
	if err != nil {
		return ValidationResult{}, err
	}
	if !ok || record.RevokedAt != nil {
		return ValidationResult{Valid: false}, nil
	}
	if requiredScope != "" && !hasScope(record.Scopes, requiredScope) {
		return ValidationResult{Valid: false}, nil
	}

	return ValidationResult{
		Valid:   true,
		TokenID: record.ID,
		Scopes:  cloneScopes(record.Scopes),
	}, nil
}

func (s *Service) ListTokens(ctx context.Context) (ListTokensResponse, error) {
	if s == nil || s.store == nil {
		return ListTokensResponse{}, errors.New("token store is required")
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return ListTokensResponse{}, err
	}
	out := make([]TokenSummary, 0, len(records))
	for _, record := range records {
		out = append(out, TokenSummary{
			ID:        record.ID,
			Name:      record.Name,
			Scopes:    cloneScopes(record.Scopes),
			CreatedAt: record.CreatedAt,
			RevokedAt: cloneTimePtr(record.RevokedAt),
		})
	}
	return ListTokensResponse{Tokens: out}, nil
}

func (s *Service) RevokeToken(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return errors.New("token store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return s.store.Revoke(ctx, id, s.now())
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomSecret(prefix string, bytesLen int) (string, error) {
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate token secret: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func normalizeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
