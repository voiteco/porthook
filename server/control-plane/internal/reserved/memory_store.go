// SPDX-License-Identifier: AGPL-3.0-only

package reserved

import (
	"context"
	"errors"
	"sort"
	"sync"
)

var ErrReservationAlreadyExists = errors.New("reserved subdomain already exists")

type MemoryStore struct {
	mu     sync.RWMutex
	byID   map[string]ReservationRecord
	byName map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:   make(map[string]ReservationRecord),
		byName: make(map[string]string),
	}
}

func (s *MemoryStore) Ping(context.Context) error {
	return nil
}

func (s *MemoryStore) Create(_ context.Context, record ReservationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byID[record.ID]; exists {
		return ErrReservationAlreadyExists
	}
	if _, exists := s.byName[record.Name]; exists {
		return ErrReservationAlreadyExists
	}

	s.byID[record.ID] = record
	s.byName[record.Name] = record.ID
	return nil
}

func (s *MemoryStore) List(context.Context) ([]ReservationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]ReservationRecord, 0, len(s.byID))
	for _, record := range s.byID {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Name == records[j].Name {
			return records[i].ID < records[j].ID
		}
		return records[i].Name < records[j].Name
	})
	return records, nil
}

func (s *MemoryStore) LookupByID(_ context.Context, id string) (ReservationRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.byID[id]
	return record, ok, nil
}

func (s *MemoryStore) LookupByName(_ context.Context, name string) (ReservationRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byName[name]
	if !ok {
		return ReservationRecord{}, false, nil
	}
	record, ok := s.byID[id]
	return record, ok, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.byID[id]
	if !ok {
		return nil
	}
	delete(s.byID, id)
	delete(s.byName, record.Name)
	return nil
}
