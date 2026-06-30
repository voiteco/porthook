// SPDX-License-Identifier: AGPL-3.0-only

package reserved

import (
	"context"
	"time"
)

type ReservationRecord struct {
	ID        string
	Name      string
	TokenID   string
	CreatedAt time.Time
}

type CreateReservationRequest struct {
	Name    string `json:"name"`
	TokenID string `json:"token_id"`
}

type ReservationSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenID   string    `json:"token_id"`
	CreatedAt time.Time `json:"created_at"`
}

type CreatedReservation struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenID   string    `json:"token_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ListReservationsResponse struct {
	ReservedSubdomains []ReservationSummary `json:"reserved_subdomains"`
}

type AuthorizationResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

type Store interface {
	Ping(context.Context) error
	Create(context.Context, ReservationRecord) error
	List(context.Context) ([]ReservationRecord, error)
	LookupByID(context.Context, string) (ReservationRecord, bool, error)
	LookupByName(context.Context, string) (ReservationRecord, bool, error)
	Delete(context.Context, string) error
}
