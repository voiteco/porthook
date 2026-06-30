// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"context"
	"errors"
	"sort"
	"sync"
)

var ErrPolicyAlreadyExists = errors.New("access policy already exists")

type MemoryStore struct {
	mu            sync.RWMutex
	byID          map[string]PolicyRecord
	byReservation map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:          make(map[string]PolicyRecord),
		byReservation: make(map[string]string),
	}
}

func (s *MemoryStore) Ping(context.Context) error {
	return nil
}

func (s *MemoryStore) Create(_ context.Context, record PolicyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byID[record.ID]; exists {
		return ErrPolicyAlreadyExists
	}
	if _, exists := s.byReservation[record.ReservedSubdomainID]; exists {
		return ErrPolicyAlreadyExists
	}

	record = cloneRecord(record)
	s.byID[record.ID] = record
	s.byReservation[record.ReservedSubdomainID] = record.ID
	return nil
}

func (s *MemoryStore) Update(_ context.Context, record PolicyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.byID[record.ID]
	if !exists {
		return ErrPolicyNotFound
	}
	if existing.ReservedSubdomainID != record.ReservedSubdomainID {
		return ErrPolicyReservedSubdomainIDRequired
	}

	record = cloneRecord(record)
	s.byID[record.ID] = record
	s.byReservation[record.ReservedSubdomainID] = record.ID
	return nil
}

func (s *MemoryStore) List(context.Context) ([]PolicyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]PolicyRecord, 0, len(s.byID))
	for _, record := range s.byID {
		records = append(records, cloneRecord(record))
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].ReservedSubdomainID == records[j].ReservedSubdomainID {
			return records[i].ID < records[j].ID
		}
		return records[i].ReservedSubdomainID < records[j].ReservedSubdomainID
	})
	return records, nil
}

func (s *MemoryStore) LookupByID(_ context.Context, id string) (PolicyRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.byID[id]
	if !ok {
		return PolicyRecord{}, false, nil
	}
	return cloneRecord(record), true, nil
}

func (s *MemoryStore) LookupByReservedSubdomainID(_ context.Context, reservedSubdomainID string) (PolicyRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byReservation[reservedSubdomainID]
	if !ok {
		return PolicyRecord{}, false, nil
	}
	record, ok := s.byID[id]
	if !ok {
		return PolicyRecord{}, false, nil
	}
	return cloneRecord(record), true, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.byID[id]
	if !ok {
		return nil
	}
	delete(s.byID, id)
	delete(s.byReservation, record.ReservedSubdomainID)
	return nil
}
