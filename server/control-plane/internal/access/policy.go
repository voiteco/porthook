// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"context"
	"time"
)

type PolicyMode string

const (
	ModePublic      PolicyMode = "public"
	ModeBasicAuth   PolicyMode = "basic_auth"
	ModeBearerToken PolicyMode = "bearer_token"
	ModeIPAllowlist PolicyMode = "ip_allowlist"
)

type PolicyRecord struct {
	ID                  string
	ReservedSubdomainID string
	Mode                PolicyMode
	BasicUsername       string
	SecretHash          string
	IPAllowlist         []string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreatePolicyRequest struct {
	ReservedSubdomainID string   `json:"reserved_subdomain_id"`
	Mode                string   `json:"mode"`
	BasicUsername       string   `json:"basic_username,omitempty"`
	BasicPassword       string   `json:"basic_password,omitempty"`
	BearerToken         string   `json:"bearer_token,omitempty"`
	IPAllowlist         []string `json:"ip_allowlist,omitempty"`
}

type UpdatePolicyRequest struct {
	Mode          string   `json:"mode"`
	BasicUsername string   `json:"basic_username,omitempty"`
	BasicPassword string   `json:"basic_password,omitempty"`
	BearerToken   string   `json:"bearer_token,omitempty"`
	IPAllowlist   []string `json:"ip_allowlist,omitempty"`
}

type PolicySummary struct {
	ID                  string     `json:"id"`
	ReservedSubdomainID string     `json:"reserved_subdomain_id"`
	Mode                PolicyMode `json:"mode"`
	BasicUsername       string     `json:"basic_username,omitempty"`
	SecretConfigured    bool       `json:"secret_configured,omitempty"`
	IPAllowlist         []string   `json:"ip_allowlist,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ListPoliciesResponse struct {
	AccessPolicies []PolicySummary `json:"access_policies"`
}

type Store interface {
	Ping(context.Context) error
	Create(context.Context, PolicyRecord) error
	Update(context.Context, PolicyRecord) error
	List(context.Context) ([]PolicyRecord, error)
	LookupByID(context.Context, string) (PolicyRecord, bool, error)
	LookupByReservedSubdomainID(context.Context, string) (PolicyRecord, bool, error)
	Delete(context.Context, string) error
}
