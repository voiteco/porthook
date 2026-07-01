// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
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
	now := s.now()
	record := DomainRecord{
		ID:                  id,
		Hostname:            hostname,
		ReservedSubdomainID: reservedSubdomainID,
		Status:              StatusActive,
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
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
}

func randomDomainID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate custom domain id: %w", err)
	}
	return "cd_" + base64.RawURLEncoding.EncodeToString(data), nil
}
