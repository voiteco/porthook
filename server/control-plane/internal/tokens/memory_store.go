// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrTokenAlreadyExists = errors.New("token already exists")

type MemoryStore struct {
	mu     sync.RWMutex
	byID   map[string]TokenRecord
	byHash map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:   make(map[string]TokenRecord),
		byHash: make(map[string]string),
	}
}

func (s *MemoryStore) Create(_ context.Context, record TokenRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byID[record.ID]; exists {
		return ErrTokenAlreadyExists
	}
	if _, exists := s.byHash[record.TokenHash]; exists {
		return ErrTokenAlreadyExists
	}

	record.Scopes = cloneScopes(record.Scopes)
	s.byID[record.ID] = record
	s.byHash[record.TokenHash] = record.ID
	return nil
}

func (s *MemoryStore) LookupByHash(_ context.Context, tokenHash string) (TokenRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byHash[tokenHash]
	if !ok {
		return TokenRecord{}, false, nil
	}
	record, ok := s.byID[id]
	if !ok {
		return TokenRecord{}, false, nil
	}
	return cloneRecord(record), true, nil
}

func (s *MemoryStore) List(_ context.Context) ([]TokenRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]TokenRecord, 0, len(s.byID))
	for _, record := range s.byID {
		records = append(records, cloneRecord(record))
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records, nil
}

func (s *MemoryStore) LookupByID(_ context.Context, id string) (TokenRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.byID[id]
	if !ok {
		return TokenRecord{}, false, nil
	}
	return cloneRecord(record), true, nil
}

func (s *MemoryStore) Revoke(_ context.Context, id string, revokedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.byID[id]
	if !ok {
		return nil
	}
	record.RevokedAt = &revokedAt
	s.byID[id] = record
	return nil
}

func cloneRecord(record TokenRecord) TokenRecord {
	record.Scopes = cloneScopes(record.Scopes)
	record.RevokedAt = cloneTimePtr(record.RevokedAt)
	return record
}

func cloneScopes(scopes []string) []string {
	return append([]string(nil), scopes...)
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
