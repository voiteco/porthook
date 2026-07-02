// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryAuditEventStoreReturnsNewestFirst(t *testing.T) {
	store := NewMemoryAuditEventStore(2)
	base := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for _, event := range []AuditEvent{
		{Time: base, Event: "first"},
		{Time: base.Add(time.Second), Event: "second"},
		{Time: base.Add(2 * time.Second), Event: "third"},
	} {
		if err := store.Add(context.Background(), event); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
	}

	events, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Event != "third" || events[1].Event != "second" {
		t.Fatalf("events = %+v, want newest retained entries", events)
	}
}

func TestAuditPostgresMigrationsAreVersioned(t *testing.T) {
	migrations := AuditPostgresMigrations()
	if len(migrations) != 1 {
		t.Fatalf("migrations = %d, want 1", len(migrations))
	}
	if migrations[0].Version != 8 {
		t.Fatalf("migration version = %d, want 8", migrations[0].Version)
	}
	if migrations[0].Name != "create_audit_events" {
		t.Fatalf("migration name = %q, want create_audit_events", migrations[0].Name)
	}
	for _, want := range []string{"audit_events", "fields_json", "request_id"} {
		if !strings.Contains(migrations[0].SQL, want) {
			t.Fatalf("migration SQL = %q, want %q", migrations[0].SQL, want)
		}
	}
}

func TestNewPostgresAuditEventStoreRejectsNilDB(t *testing.T) {
	if _, err := NewPostgresAuditEventStore(nil); err == nil {
		t.Fatal("NewPostgresAuditEventStore returned nil error")
	}
}

func TestAuditEventFieldsEncodingRoundTrip(t *testing.T) {
	raw, err := encodeAuditEventFields(map[string]string{"token_id": "tok_demo"})
	if err != nil {
		t.Fatalf("encodeAuditEventFields returned error: %v", err)
	}
	fields, err := decodeAuditEventFields(raw)
	if err != nil {
		t.Fatalf("decodeAuditEventFields returned error: %v", err)
	}
	if fields["token_id"] != "tok_demo" {
		t.Fatalf("fields = %+v, want token_id", fields)
	}
}
