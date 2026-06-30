// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
)

var (
	ErrPolicyReservedSubdomainIDRequired = errors.New("reserved_subdomain_id is required")
	ErrPolicyModeRequired                = errors.New("access policy mode is required")
	ErrPolicyModeUnsupported             = errors.New("unsupported access policy mode")
	ErrPolicySecretRequired              = errors.New("access policy secret is required")
	ErrPolicyBasicUsernameRequired       = errors.New("basic auth username is required")
	ErrPolicyIPAllowlistRequired         = errors.New("ip allowlist is required")
	ErrPolicyNotFound                    = errors.New("access policy not found")
)

var supportedModes = []PolicyMode{
	ModePublic,
	ModeBasicAuth,
	ModeBearerToken,
	ModeIPAllowlist,
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

func (s *Service) Ready(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("access policy store is required")
	}
	return s.store.Ping(ctx)
}

func (s *Service) CreatePolicy(ctx context.Context, req CreatePolicyRequest) (PolicySummary, error) {
	if s == nil || s.store == nil {
		return PolicySummary{}, errors.New("access policy store is required")
	}

	record, err := newPolicyRecord(req, s.now())
	if err != nil {
		return PolicySummary{}, err
	}
	id, err := randomPolicyID()
	if err != nil {
		return PolicySummary{}, err
	}
	record.ID = id

	if err := s.store.Create(ctx, record); err != nil {
		return PolicySummary{}, err
	}
	return summarize(record), nil
}

func (s *Service) UpdatePolicy(ctx context.Context, id string, req UpdatePolicyRequest) (PolicySummary, error) {
	if s == nil || s.store == nil {
		return PolicySummary{}, errors.New("access policy store is required")
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return PolicySummary{}, ErrPolicyNotFound
	}
	existing, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return PolicySummary{}, err
	}
	if !ok {
		return PolicySummary{}, ErrPolicyNotFound
	}

	record, err := updatedPolicyRecord(existing, req, s.now())
	if err != nil {
		return PolicySummary{}, err
	}
	record.ID = existing.ID
	record.CreatedAt = existing.CreatedAt

	if err := s.store.Update(ctx, record); err != nil {
		return PolicySummary{}, err
	}
	return summarize(record), nil
}

func (s *Service) ListPolicies(ctx context.Context) (ListPoliciesResponse, error) {
	if s == nil || s.store == nil {
		return ListPoliciesResponse{}, errors.New("access policy store is required")
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return ListPoliciesResponse{}, err
	}

	out := make([]PolicySummary, 0, len(records))
	for _, record := range records {
		out = append(out, summarize(record))
	}
	return ListPoliciesResponse{AccessPolicies: out}, nil
}

func (s *Service) GetPolicy(ctx context.Context, id string) (PolicySummary, bool, error) {
	if s == nil || s.store == nil {
		return PolicySummary{}, false, errors.New("access policy store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return PolicySummary{}, false, nil
	}
	record, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return PolicySummary{}, false, err
	}
	if !ok {
		return PolicySummary{}, false, nil
	}
	return summarize(record), true, nil
}

func (s *Service) GetPolicyForReservedSubdomain(ctx context.Context, reservedSubdomainID string) (PolicySummary, bool, error) {
	if s == nil || s.store == nil {
		return PolicySummary{}, false, errors.New("access policy store is required")
	}
	reservedSubdomainID = strings.TrimSpace(reservedSubdomainID)
	if reservedSubdomainID == "" {
		return PolicySummary{}, false, nil
	}
	record, ok, err := s.store.LookupByReservedSubdomainID(ctx, reservedSubdomainID)
	if err != nil {
		return PolicySummary{}, false, err
	}
	if !ok {
		return PolicySummary{}, false, nil
	}
	return summarize(record), true, nil
}

func (s *Service) DeletePolicy(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return errors.New("access policy store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrPolicyNotFound
	}
	_, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPolicyNotFound
	}
	return s.store.Delete(ctx, id)
}

func (s *Service) CheckPolicy(ctx context.Context, req CheckPolicyRequest) (CheckPolicyResult, error) {
	if s == nil || s.store == nil {
		return CheckPolicyResult{}, errors.New("access policy store is required")
	}
	reservedSubdomainID := strings.TrimSpace(req.ReservedSubdomainID)
	if reservedSubdomainID == "" {
		return CheckPolicyResult{}, ErrPolicyReservedSubdomainIDRequired
	}

	record, ok, err := s.store.LookupByReservedSubdomainID(ctx, reservedSubdomainID)
	if err != nil {
		return CheckPolicyResult{}, err
	}
	if !ok {
		return CheckPolicyResult{Allowed: true, Mode: ModePublic, Reason: "no_policy"}, nil
	}

	switch record.Mode {
	case ModePublic:
		return CheckPolicyResult{Allowed: true, Mode: record.Mode, Reason: "public"}, nil
	case ModeBasicAuth:
		if !basicAuthMatches(record, req.BasicUsername, req.BasicPassword) {
			return CheckPolicyResult{Allowed: false, Mode: record.Mode, Reason: "basic_auth_required"}, nil
		}
		return CheckPolicyResult{Allowed: true, Mode: record.Mode}, nil
	case ModeBearerToken:
		if !secretHashEqual(HashSecret(strings.TrimSpace(req.BearerToken)), record.SecretHash) {
			return CheckPolicyResult{Allowed: false, Mode: record.Mode, Reason: "bearer_token_required"}, nil
		}
		return CheckPolicyResult{Allowed: true, Mode: record.Mode}, nil
	case ModeIPAllowlist:
		if !ipAllowed(req.RemoteIP, record.IPAllowlist) {
			return CheckPolicyResult{Allowed: false, Mode: record.Mode, Reason: "ip_not_allowed"}, nil
		}
		return CheckPolicyResult{Allowed: true, Mode: record.Mode}, nil
	default:
		return CheckPolicyResult{Allowed: false, Mode: record.Mode, Reason: "unsupported_policy_mode"}, nil
	}
}

func newPolicyRecord(req CreatePolicyRequest, now time.Time) (PolicyRecord, error) {
	reservedSubdomainID := strings.TrimSpace(req.ReservedSubdomainID)
	if reservedSubdomainID == "" {
		return PolicyRecord{}, ErrPolicyReservedSubdomainIDRequired
	}

	mode, err := NormalizeMode(req.Mode)
	if err != nil {
		return PolicyRecord{}, err
	}

	record := PolicyRecord{
		ReservedSubdomainID: reservedSubdomainID,
		Mode:                mode,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	switch mode {
	case ModePublic:
		return record, nil
	case ModeBasicAuth:
		username := strings.TrimSpace(req.BasicUsername)
		if username == "" {
			return PolicyRecord{}, ErrPolicyBasicUsernameRequired
		}
		password := strings.TrimSpace(req.BasicPassword)
		if password == "" {
			return PolicyRecord{}, ErrPolicySecretRequired
		}
		record.BasicUsername = username
		record.SecretHash = HashSecret(password)
	case ModeBearerToken:
		token := strings.TrimSpace(req.BearerToken)
		if token == "" {
			return PolicyRecord{}, ErrPolicySecretRequired
		}
		record.SecretHash = HashSecret(token)
	case ModeIPAllowlist:
		allowlist, err := NormalizeIPAllowlist(req.IPAllowlist)
		if err != nil {
			return PolicyRecord{}, err
		}
		record.IPAllowlist = allowlist
	default:
		return PolicyRecord{}, fmt.Errorf("%w %q", ErrPolicyModeUnsupported, mode)
	}
	return record, nil
}

func updatedPolicyRecord(existing PolicyRecord, req UpdatePolicyRequest, now time.Time) (PolicyRecord, error) {
	modeText := strings.TrimSpace(req.Mode)
	if modeText == "" {
		modeText = string(existing.Mode)
	}
	mode, err := NormalizeMode(modeText)
	if err != nil {
		return PolicyRecord{}, err
	}

	record := PolicyRecord{
		ID:                  existing.ID,
		ReservedSubdomainID: existing.ReservedSubdomainID,
		Mode:                mode,
		CreatedAt:           existing.CreatedAt,
		UpdatedAt:           now,
	}
	switch mode {
	case ModePublic:
		return record, nil
	case ModeBasicAuth:
		username := strings.TrimSpace(req.BasicUsername)
		if username == "" && existing.Mode == ModeBasicAuth {
			username = existing.BasicUsername
		}
		if username == "" {
			return PolicyRecord{}, ErrPolicyBasicUsernameRequired
		}
		record.BasicUsername = username
		password := strings.TrimSpace(req.BasicPassword)
		if password != "" {
			record.SecretHash = HashSecret(password)
		} else if existing.Mode == ModeBasicAuth && existing.SecretHash != "" {
			record.SecretHash = existing.SecretHash
		} else {
			return PolicyRecord{}, ErrPolicySecretRequired
		}
	case ModeBearerToken:
		token := strings.TrimSpace(req.BearerToken)
		if token != "" {
			record.SecretHash = HashSecret(token)
		} else if existing.Mode == ModeBearerToken && existing.SecretHash != "" {
			record.SecretHash = existing.SecretHash
		} else {
			return PolicyRecord{}, ErrPolicySecretRequired
		}
	case ModeIPAllowlist:
		if len(req.IPAllowlist) == 0 && existing.Mode == ModeIPAllowlist && len(existing.IPAllowlist) > 0 {
			record.IPAllowlist = cloneStrings(existing.IPAllowlist)
			return record, nil
		}
		allowlist, err := NormalizeIPAllowlist(req.IPAllowlist)
		if err != nil {
			return PolicyRecord{}, err
		}
		record.IPAllowlist = allowlist
	default:
		return PolicyRecord{}, fmt.Errorf("%w %q", ErrPolicyModeUnsupported, mode)
	}
	return record, nil
}

func NormalizeMode(mode string) (PolicyMode, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "", ErrPolicyModeRequired
	}
	policyMode := PolicyMode(mode)
	if !slices.Contains(supportedModes, policyMode) {
		return "", fmt.Errorf("%w %q", ErrPolicyModeUnsupported, mode)
	}
	return policyMode, nil
}

func NormalizeIPAllowlist(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		normalized, err := normalizeIPAllowlistValue(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, ErrPolicyIPAllowlistRequired
	}
	return out, nil
}

func normalizeIPAllowlistValue(value string) (string, error) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked().String(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", fmt.Errorf("invalid ip allowlist entry %q", value)
	}
	return addr.String(), nil
}

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func basicAuthMatches(record PolicyRecord, username, password string) bool {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" || record.BasicUsername == "" || record.SecretHash == "" {
		return false
	}
	return secretEqual(username, record.BasicUsername) && secretHashEqual(HashSecret(password), record.SecretHash)
}

func secretEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

func secretHashEqual(gotHash, wantHash string) bool {
	if gotHash == "" || wantHash == "" || len(gotHash) != len(wantHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(gotHash), []byte(wantHash)) == 1
}

func ipAllowed(remoteIP string, allowlist []string) bool {
	remoteIP = strings.TrimSpace(remoteIP)
	if remoteIP == "" {
		return false
	}
	addr, err := netip.ParseAddr(remoteIP)
	if err != nil {
		return false
	}
	for _, value := range allowlist {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			if prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(value)
		if err == nil && allowedAddr == addr {
			return true
		}
	}
	return false
}

func summarize(record PolicyRecord) PolicySummary {
	return PolicySummary{
		ID:                  record.ID,
		ReservedSubdomainID: record.ReservedSubdomainID,
		Mode:                record.Mode,
		BasicUsername:       record.BasicUsername,
		SecretConfigured:    record.SecretHash != "",
		IPAllowlist:         cloneStrings(record.IPAllowlist),
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
}

func randomPolicyID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate access policy id: %w", err)
	}
	return "ap_" + base64.RawURLEncoding.EncodeToString(data), nil
}

func cloneRecord(record PolicyRecord) PolicyRecord {
	record.IPAllowlist = cloneStrings(record.IPAllowlist)
	return record
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
