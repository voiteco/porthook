// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type Type string

const (
	TypeAuthRequest       Type = "auth.request"
	TypeAuthOK            Type = "auth.ok"
	TypeAuthError         Type = "auth.error"
	TypeTunnelRegister    Type = "tunnel.register"
	TypeTunnelRegistered  Type = "tunnel.registered"
	TypeTunnelError       Type = "tunnel.error"
	TypePing              Type = "ping"
	TypePong              Type = "pong"
	TypeHTTPRequest       Type = "http.request"
	TypeHTTPRequestStart  Type = "http.request.start"
	TypeHTTPRequestBody   Type = "http.request.body"
	TypeHTTPRequestEnd    Type = "http.request.end"
	TypeHTTPResponse      Type = "http.response"
	TypeHTTPResponseStart Type = "http.response.start"
	TypeHTTPResponseBody  Type = "http.response.body"
	TypeHTTPResponseEnd   Type = "http.response.end"
	TypeHTTPStreamError   Type = "http.stream.error"
	TypeHTTPStreamCancel  Type = "http.stream.cancel"
)

const ProtocolVersion = "0.2"

const (
	CapabilityStreamStartEnd  = "stream_start_end"
	CapabilityBinaryBodyFrame = "binary_body_frames"
	CapabilityStreamCancel    = "stream_cancel"
)

var defaultProtocolCapabilities = []string{
	CapabilityStreamStartEnd,
	CapabilityBinaryBodyFrame,
	CapabilityStreamCancel,
}

type Envelope struct {
	Type     Type            `json:"type"`
	StreamID string          `json:"stream_id,omitempty"`
	TunnelID string          `json:"tunnel_id,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

type AuthRequest struct {
	Token           string   `json:"token"`
	AgentVersion    string   `json:"agent_version,omitempty"`
	ProtocolVersion string   `json:"protocol_version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

type AuthOK struct {
	ProtocolVersion string   `json:"protocol_version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

func DefaultProtocolCapabilities() []string {
	return append([]string(nil), defaultProtocolCapabilities...)
}

func MissingRequiredCapabilities(capabilities []string, required []string) []string {
	if len(required) == 0 {
		return nil
	}

	missingMap := make(map[string]struct{}, len(required))
	for _, capability := range required {
		missingMap[capability] = struct{}{}
	}
	for _, capability := range capabilities {
		delete(missingMap, capability)
	}

	missing := make([]string, 0, len(missingMap))
	for capability := range missingMap {
		missing = append(missing, capability)
	}
	sort.Strings(missing)
	return missing
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TunnelRegister struct {
	Protocol           string `json:"protocol"`
	LocalTarget        string `json:"local_target"`
	RequestedSubdomain string `json:"requested_subdomain,omitempty"`
}

type TunnelRegistered struct {
	TunnelID  string `json:"tunnel_id"`
	PublicURL string `json:"public_url"`
	Subdomain string `json:"subdomain"`
}

type StreamCancel struct {
	Reason string `json:"reason"`
}

func New(typ Type, payload any) (Envelope, error) {
	return NewStream(typ, "", "", payload)
}

func NewStream(typ Type, streamID, tunnelID string, payload any) (Envelope, error) {
	if typ == "" {
		return Envelope{}, errors.New("message type is required")
	}

	raw, err := marshalPayload(payload)
	if err != nil {
		return Envelope{}, err
	}

	return Envelope{
		Type:     typ,
		StreamID: streamID,
		TunnelID: tunnelID,
		Payload:  raw,
	}, nil
}

func DecodePayload[T any](env Envelope) (T, error) {
	var payload T
	if len(env.Payload) == 0 {
		return payload, errors.New("payload is required")
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return payload, fmt.Errorf("decode %s payload: %w", env.Type, err)
	}
	return payload, nil
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return json.RawMessage(`{}`), nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	return raw, nil
}
