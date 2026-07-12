// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var (
	ErrDomainHostnameRequired            = errors.New("custom domain hostname is required")
	ErrDomainReservedSubdomainIDRequired = errors.New("reserved_subdomain_id is required")
	ErrDomainInvalidHostname             = errors.New("invalid custom domain hostname")
	ErrDomainNotFound                    = errors.New("custom domain not found")
)

type Service struct {
	store    Store
	now      func() time.Time
	resolver TXTResolver
}

func NewService(store Store) *Service {
	return NewServiceWithResolver(store, netTXTResolver{})
}

func NewServiceWithResolver(store Store, resolver TXTResolver) *Service {
	if resolver == nil {
		resolver = netTXTResolver{}
	}
	return &Service{
		store:    store,
		now:      func() time.Time { return time.Now().UTC() },
		resolver: resolver,
	}
}

func (s *Service) Ready(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("custom domain store is required")
	}
	return s.store.Ping(ctx)
}

func (s *Service) CreateDomain(ctx context.Context, req CreateDomainRequest) (CreatedDomain, error) {
	if s == nil || s.store == nil {
		return CreatedDomain{}, errors.New("custom domain store is required")
	}

	hostname, err := NormalizeHostname(req.Hostname)
	if err != nil {
		return CreatedDomain{}, err
	}
	reservedSubdomainID := strings.TrimSpace(req.ReservedSubdomainID)
	if reservedSubdomainID == "" {
		return CreatedDomain{}, ErrDomainReservedSubdomainIDRequired
	}
	id, err := randomDomainID()
	if err != nil {
		return CreatedDomain{}, err
	}
	verificationToken, err := randomVerificationToken()
	if err != nil {
		return CreatedDomain{}, err
	}
	now := s.now()
	record := DomainRecord{
		ID:                  id,
		Hostname:            hostname,
		ReservedSubdomainID: reservedSubdomainID,
		Status:              StatusPendingVerification,
		VerificationToken:   verificationToken,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.store.Create(ctx, record); err != nil {
		if errors.Is(err, ErrDomainAlreadyExists) {
			return CreatedDomain{}, err
		}
		return CreatedDomain{}, fmt.Errorf("create custom domain: %w", err)
	}
	summary := summarize(record)
	return CreatedDomain(summary), nil
}

func (s *Service) ListDomains(ctx context.Context) (ListDomainsResponse, error) {
	if s == nil || s.store == nil {
		return ListDomainsResponse{}, errors.New("custom domain store is required")
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return ListDomainsResponse{}, err
	}

	out := make([]DomainSummary, 0, len(records))
	for _, record := range records {
		out = append(out, summarize(record))
	}
	return ListDomainsResponse{CustomDomains: out}, nil
}

func (s *Service) VerifyDomain(ctx context.Context, id string) (VerifyDomainResponse, error) {
	if s == nil || s.store == nil {
		return VerifyDomainResponse{}, errors.New("custom domain store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return VerifyDomainResponse{}, ErrDomainNotFound
	}

	record, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return VerifyDomainResponse{}, err
	}
	if !ok {
		return VerifyDomainResponse{}, ErrDomainNotFound
	}
	if record.Status == StatusActive {
		return VerifyDomainResponse(summarize(record)), nil
	}
	if strings.TrimSpace(record.VerificationToken) == "" {
		return VerifyDomainResponse{}, errors.New("custom domain verification token is empty")
	}

	status := StatusVerificationFailed
	verifiedAt := record.VerifiedAt
	txtValues, err := s.resolver.LookupTXT(ctx, VerificationName(record.Hostname))
	now := s.now()
	if err == nil && verificationTXTMatches(txtValues, record.VerificationToken) {
		status = StatusActive
		verifiedAt = &now
	}

	record.Status = status
	record.VerifiedAt = verifiedAt
	record.UpdatedAt = now
	if err := s.store.Update(ctx, record); err != nil {
		return VerifyDomainResponse{}, fmt.Errorf("verify custom domain: %w", err)
	}
	return VerifyDomainResponse(summarize(record)), nil
}

func (s *Service) GetDomain(ctx context.Context, id string) (DomainSummary, bool, error) {
	if s == nil || s.store == nil {
		return DomainSummary{}, false, errors.New("custom domain store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return DomainSummary{}, false, nil
	}
	record, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return DomainSummary{}, false, err
	}
	if !ok {
		return DomainSummary{}, false, nil
	}
	return summarize(record), true, nil
}

func (s *Service) GetDomainByHostname(ctx context.Context, hostname string) (DomainSummary, bool, error) {
	if s == nil || s.store == nil {
		return DomainSummary{}, false, errors.New("custom domain store is required")
	}
	hostname, err := NormalizeHostname(hostname)
	if err != nil {
		return DomainSummary{}, false, err
	}
	record, ok, err := s.store.LookupByHostname(ctx, hostname)
	if err != nil {
		return DomainSummary{}, false, err
	}
	if !ok {
		return DomainSummary{}, false, nil
	}
	return summarize(record), true, nil
}

func (s *Service) DeleteDomain(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return errors.New("custom domain store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrDomainNotFound
	}
	_, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDomainNotFound
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete custom domain: %w", err)
	}
	return nil
}

func NormalizeHostname(hostname string) (string, error) {
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if hostname == "" {
		return "", ErrDomainHostnameRequired
	}
	if strings.Contains(hostname, ":") || strings.Contains(hostname, "*") {
		return "", fmt.Errorf("%w %q", ErrDomainInvalidHostname, hostname)
	}
	if len(hostname) > 253 {
		return "", fmt.Errorf("%w %q: hostname must be at most 253 characters", ErrDomainInvalidHostname, hostname)
	}
	if !strings.Contains(hostname, ".") {
		return "", fmt.Errorf("%w %q: hostname must contain at least one dot", ErrDomainInvalidHostname, hostname)
	}
	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if err := validateHostnameLabel(label); err != nil {
			return "", fmt.Errorf("%w %q: %w", ErrDomainInvalidHostname, hostname, err)
		}
	}
	return hostname, nil
}

func validateHostnameLabel(label string) error {
	if label == "" {
		return errors.New("hostname labels cannot be empty")
	}
	if len(label) > 63 {
		return errors.New("hostname labels must be at most 63 characters")
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return errors.New("hostname labels cannot start or end with hyphen")
	}
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return errors.New("hostname labels can contain only lowercase ASCII letters, numbers, and hyphens")
		}
	}
	return nil
}

func summarize(record DomainRecord) DomainSummary {
	return DomainSummary{
		ID:                  record.ID,
		Hostname:            record.Hostname,
		ReservedSubdomainID: record.ReservedSubdomainID,
		Status:              record.Status,
		VerificationToken:   record.VerificationToken,
		VerificationName:    VerificationName(record.Hostname),
		VerifiedAt:          record.VerifiedAt,
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
}

func VerificationName(hostname string) string {
	return "_porthook." + strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
}

func verificationTXTMatches(values []string, token string) bool {
	expected := "porthook-domain-verification=" + token
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == token || value == expected {
			return true
		}
	}
	return false
}

func randomDomainID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate custom domain id: %w", err)
	}
	return "cd_" + base64.RawURLEncoding.EncodeToString(data), nil
}

func randomVerificationToken() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate custom domain verification token: %w", err)
	}
	return "phdv_" + base64.RawURLEncoding.EncodeToString(data), nil
}

type TXTResolver interface {
	LookupTXT(context.Context, string) ([]string, error)
}

type netTXTResolver struct{}

func (netTXTResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

// NewTXTResolver returns the system DNS resolver when addr is empty, or a
// resolver that sends every TXT query to the given "host:port" DNS server
// instead. Use a custom addr for split-horizon DNS, a private authoritative
// zone, or local testing.
func NewTXTResolver(addr string) TXTResolver {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return netTXTResolver{}
	}
	return &customTXTResolver{addr: addr}
}

type customTXTResolver struct {
	addr string
}

func (r *customTXTResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, r.addr)
		},
	}
	return resolver.LookupTXT(ctx, name)
}
