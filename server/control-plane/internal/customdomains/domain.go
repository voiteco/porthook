// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"time"
)

type DomainStatus string

const (
	StatusActive DomainStatus = "active"
)

type DomainRecord struct {
	ID                  string
	Hostname            string
	ReservedSubdomainID string
	Status              DomainStatus
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateDomainRequest struct {
	Hostname            string `json:"hostname"`
	ReservedSubdomainID string `json:"reserved_subdomain_id"`
}

type DomainSummary struct {
	ID                  string       `json:"id"`
	Hostname            string       `json:"hostname"`
	ReservedSubdomainID string       `json:"reserved_subdomain_id"`
	Status              DomainStatus `json:"status"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type CreatedDomain struct {
	ID                  string       `json:"id"`
	Hostname            string       `json:"hostname"`
	ReservedSubdomainID string       `json:"reserved_subdomain_id"`
	Status              DomainStatus `json:"status"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type ListDomainsResponse struct {
	CustomDomains []DomainSummary `json:"custom_domains"`
}

type Store interface {
	Ping(context.Context) error
	Create(context.Context, DomainRecord) error
	List(context.Context) ([]DomainRecord, error)
	LookupByID(context.Context, string) (DomainRecord, bool, error)
	LookupByHostname(context.Context, string) (DomainRecord, bool, error)
	Delete(context.Context, string) error
}
