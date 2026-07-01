// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"errors"
	"sort"
	"sync"
)

var ErrDomainAlreadyExists = errors.New("custom domain already exists")

type MemoryStore struct {
	mu         sync.RWMutex
	byID       map[string]DomainRecord
	byHostname map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:       make(map[string]DomainRecord),
		byHostname: make(map[string]string),
	}
}

func (s *MemoryStore) Ping(context.Context) error {
	return nil
}

func (s *MemoryStore) Create(_ context.Context, record DomainRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byID[record.ID]; exists {
		return ErrDomainAlreadyExists
	}
	if _, exists := s.byHostname[record.Hostname]; exists {
		return ErrDomainAlreadyExists
	}

	s.byID[record.ID] = record
	s.byHostname[record.Hostname] = record.ID
	return nil
}

func (s *MemoryStore) List(context.Context) ([]DomainRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]DomainRecord, 0, len(s.byID))
	for _, record := range s.byID {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Hostname == records[j].Hostname {
			return records[i].ID < records[j].ID
		}
		return records[i].Hostname < records[j].Hostname
	})
	return records, nil
}

func (s *MemoryStore) LookupByID(_ context.Context, id string) (DomainRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.byID[id]
	return record, ok, nil
}

func (s *MemoryStore) LookupByHostname(_ context.Context, hostname string) (DomainRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byHostname[hostname]
	if !ok {
		return DomainRecord{}, false, nil
	}
	record, ok := s.byID[id]
	return record, ok, nil
}

func (s *MemoryStore) Update(_ context.Context, record DomainRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byID[record.ID]
	if !ok {
		return ErrDomainNotFound
	}
	if existing.Hostname != record.Hostname {
		if otherID, exists := s.byHostname[record.Hostname]; exists && otherID != record.ID {
			return ErrDomainAlreadyExists
		}
		delete(s.byHostname, existing.Hostname)
	}
	s.byID[record.ID] = record
	s.byHostname[record.Hostname] = record.ID
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.byID[id]
	if !ok {
		return nil
	}
	delete(s.byID, id)
	delete(s.byHostname, record.Hostname)
	return nil
}
