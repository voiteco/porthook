// SPDX-License-Identifier: AGPL-3.0-only

package reserved

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/voiteco/porthook/protocol/names"
)

var (
	ErrReservationNameRequired    = errors.New("reserved subdomain name is required")
	ErrReservationTokenIDRequired = errors.New("reserved subdomain token_id is required")
	ErrReservationNotFound        = errors.New("reserved subdomain not found")
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
		return errors.New("reserved subdomain store is required")
	}
	return s.store.Ping(ctx)
}

func (s *Service) CreateReservation(ctx context.Context, req CreateReservationRequest) (CreatedReservation, error) {
	if s == nil || s.store == nil {
		return CreatedReservation{}, errors.New("reserved subdomain store is required")
	}

	name, err := NormalizeName(req.Name)
	if err != nil {
		return CreatedReservation{}, err
	}
	tokenID := strings.TrimSpace(req.TokenID)
	if tokenID == "" {
		return CreatedReservation{}, ErrReservationTokenIDRequired
	}

	id, err := randomReservationID()
	if err != nil {
		return CreatedReservation{}, err
	}
	createdAt := s.now()
	record := ReservationRecord{
		ID:        id,
		Name:      name,
		TokenID:   tokenID,
		CreatedAt: createdAt,
	}
	if err := s.store.Create(ctx, record); err != nil {
		if errors.Is(err, ErrReservationAlreadyExists) {
			return CreatedReservation{}, err
		}
		return CreatedReservation{}, fmt.Errorf("create reserved subdomain: %w", err)
	}

	return CreatedReservation{
		ID:        id,
		Name:      name,
		TokenID:   tokenID,
		CreatedAt: createdAt,
	}, nil
}

func (s *Service) ListReservations(ctx context.Context) (ListReservationsResponse, error) {
	if s == nil || s.store == nil {
		return ListReservationsResponse{}, errors.New("reserved subdomain store is required")
	}
	records, err := s.store.List(ctx)
	if err != nil {
		return ListReservationsResponse{}, err
	}

	out := make([]ReservationSummary, 0, len(records))
	for _, record := range records {
		out = append(out, ReservationSummary{
			ID:        record.ID,
			Name:      record.Name,
			TokenID:   record.TokenID,
			CreatedAt: record.CreatedAt,
		})
	}
	return ListReservationsResponse{ReservedSubdomains: out}, nil
}

func (s *Service) DeleteReservation(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return errors.New("reserved subdomain store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrReservationNotFound
	}
	_, ok, err := s.store.LookupByID(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrReservationNotFound
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete reserved subdomain: %w", err)
	}
	return nil
}

func (s *Service) Authorize(ctx context.Context, tokenID, subdomain string) (AuthorizationResult, error) {
	if s == nil || s.store == nil {
		return AuthorizationResult{}, errors.New("reserved subdomain store is required")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return AuthorizationResult{Allowed: false, Reason: "missing_token_id"}, nil
	}
	name, err := NormalizeName(subdomain)
	if err != nil {
		return AuthorizationResult{}, err
	}
	record, ok, err := s.store.LookupByName(ctx, name)
	if err != nil {
		return AuthorizationResult{}, err
	}
	if !ok {
		return AuthorizationResult{Allowed: false, Reason: "not_reserved"}, nil
	}
	if record.TokenID != tokenID {
		return AuthorizationResult{Allowed: false, Reason: "reserved_for_another_token"}, nil
	}
	return AuthorizationResult{Allowed: true}, nil
}

func NormalizeName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", ErrReservationNameRequired
	}
	if err := names.ValidateSubdomain(name); err != nil {
		return "", fmt.Errorf("invalid reserved subdomain %q: %w", name, err)
	}
	return name, nil
}

func randomReservationID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate reserved subdomain id: %w", err)
	}
	return "rs_" + base64.RawURLEncoding.EncodeToString(data), nil
}
