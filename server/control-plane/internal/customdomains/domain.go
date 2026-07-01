// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"time"
)

type DomainStatus string

const (
	StatusPendingVerification DomainStatus = "pending_verification"
	StatusActive              DomainStatus = "active"
	StatusVerificationFailed  DomainStatus = "verification_failed"
)

type DomainRecord struct {
	ID                  string
	Hostname            string
	ReservedSubdomainID string
	Status              DomainStatus
	VerificationToken   string
	VerifiedAt          *time.Time
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
	VerificationToken   string       `json:"verification_token"`
	VerificationName    string       `json:"verification_name"`
	VerifiedAt          *time.Time   `json:"verified_at,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type CreatedDomain struct {
	ID                  string       `json:"id"`
	Hostname            string       `json:"hostname"`
	ReservedSubdomainID string       `json:"reserved_subdomain_id"`
	Status              DomainStatus `json:"status"`
	VerificationToken   string       `json:"verification_token"`
	VerificationName    string       `json:"verification_name"`
	VerifiedAt          *time.Time   `json:"verified_at,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type ListDomainsResponse struct {
	CustomDomains []DomainSummary `json:"custom_domains"`
}

type VerifyDomainResponse struct {
	ID                  string       `json:"id"`
	Hostname            string       `json:"hostname"`
	ReservedSubdomainID string       `json:"reserved_subdomain_id"`
	Status              DomainStatus `json:"status"`
	VerificationToken   string       `json:"verification_token"`
	VerificationName    string       `json:"verification_name"`
	VerifiedAt          *time.Time   `json:"verified_at,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

type Store interface {
	Ping(context.Context) error
	Create(context.Context, DomainRecord) error
	List(context.Context) ([]DomainRecord, error)
	LookupByID(context.Context, string) (DomainRecord, bool, error)
	LookupByHostname(context.Context, string) (DomainRecord, bool, error)
	Update(context.Context, DomainRecord) error
	Delete(context.Context, string) error
}
