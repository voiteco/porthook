// SPDX-License-Identifier: AGPL-3.0-only

package reserved

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceCreatesAndListsReservation(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())
	createdAt := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return createdAt }

	created, err := service.CreateReservation(ctx, CreateReservationRequest{
		Name:    " Demo ",
		TokenID: "tok_123",
	})
	if err != nil {
		t.Fatalf("CreateReservation returned error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created reservation id is empty")
	}
	if created.Name != "demo" {
		t.Fatalf("name = %q, want demo", created.Name)
	}
	if created.TokenID != "tok_123" {
		t.Fatalf("token_id = %q, want tok_123", created.TokenID)
	}

	listed, err := service.ListReservations(ctx)
	if err != nil {
		t.Fatalf("ListReservations returned error: %v", err)
	}
	if len(listed.ReservedSubdomains) != 1 {
		t.Fatalf("reserved_subdomains = %d, want 1", len(listed.ReservedSubdomains))
	}
	if listed.ReservedSubdomains[0].Name != "demo" {
		t.Fatalf("listed reservation = %+v, want demo", listed.ReservedSubdomains[0])
	}
}

func TestServiceRejectsInvalidReservation(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateReservation(ctx, CreateReservationRequest{
		Name:    "Bad_Name",
		TokenID: "tok_123",
	})
	if err == nil {
		t.Fatal("CreateReservation returned nil error")
	}
	if !strings.Contains(err.Error(), "can contain only lowercase ASCII") {
		t.Fatalf("error = %q, want subdomain validation guidance", err.Error())
	}

	_, err = service.CreateReservation(ctx, CreateReservationRequest{
		Name: "valid-name",
	})
	if !errors.Is(err, ErrReservationTokenIDRequired) {
		t.Fatalf("error = %v, want ErrReservationTokenIDRequired", err)
	}
}

func TestServiceRejectsDuplicateReservation(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateReservation(ctx, CreateReservationRequest{
		Name:    "demo",
		TokenID: "tok_123",
	})
	if err != nil {
		t.Fatalf("CreateReservation returned error: %v", err)
	}
	_, err = service.CreateReservation(ctx, CreateReservationRequest{
		Name:    "demo",
		TokenID: "tok_456",
	})
	if !errors.Is(err, ErrReservationAlreadyExists) {
		t.Fatalf("error = %v, want ErrReservationAlreadyExists", err)
	}
}

func TestServiceAuthorizesReservationOwner(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateReservation(ctx, CreateReservationRequest{
		Name:    "demo",
		TokenID: "tok_owner",
	})
	if err != nil {
		t.Fatalf("CreateReservation returned error: %v", err)
	}

	result, err := service.Authorize(ctx, "tok_owner", "demo")
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("result = %+v, want allowed", result)
	}

	result, err = service.Authorize(ctx, "tok_other", "demo")
	if err != nil {
		t.Fatalf("Authorize other returned error: %v", err)
	}
	if result.Allowed || result.Reason != "reserved_for_another_token" {
		t.Fatalf("result = %+v, want reserved_for_another_token", result)
	}

	result, err = service.Authorize(ctx, "tok_owner", "missing")
	if err != nil {
		t.Fatalf("Authorize missing returned error: %v", err)
	}
	if result.Allowed || result.Reason != "not_reserved" {
		t.Fatalf("result = %+v, want not_reserved", result)
	}
}

func TestServiceDeletesReservation(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateReservation(ctx, CreateReservationRequest{
		Name:    "demo",
		TokenID: "tok_owner",
	})
	if err != nil {
		t.Fatalf("CreateReservation returned error: %v", err)
	}
	if err := service.DeleteReservation(ctx, created.ID); err != nil {
		t.Fatalf("DeleteReservation returned error: %v", err)
	}

	result, err := service.Authorize(ctx, "tok_owner", "demo")
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if result.Allowed || result.Reason != "not_reserved" {
		t.Fatalf("result = %+v, want not_reserved", result)
	}
}
