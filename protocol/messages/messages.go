// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
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

// ProtocolVersion is the protocol revision this build speaks. It is
// informational and negotiated additively through capabilities: peers do not
// need to match it exactly, only to satisfy RequiredProtocolCapabilities.
const ProtocolVersion = "0.3"

// MinSupportedProtocolVersion is the oldest peer protocol version this build
// still interoperates with. Versions from 0.2 onward carry the auth
// capability-negotiation fields introduced in 0.2.0; older, unversioned
// peers cannot be identified safely and are rejected.
const MinSupportedProtocolVersion = "0.2"

const (
	CapabilityStreamStartEnd  = "stream_start_end"
	CapabilityBinaryBodyFrame = "binary_body_frames"
	CapabilityStreamCancel    = "stream_cancel"
	// CapabilityWebSocketTunnel indicates the peer can open and relay public
	// WebSocket tunnel streams (ws.open/ws.accept/ws.error/ws.close/
	// ws.cancel and WebSocket binary body frames). It is additive: a peer
	// missing it still interoperates fully for HTTP tunneling, and only
	// WebSocket upgrade attempts against its tunnels are rejected.
	CapabilityWebSocketTunnel = "websocket_tunnel"
)

// requiredProtocolCapabilities are the capabilities every supported peer
// must declare. They have been required since protocol 0.2 and define the
// v0.2 HTTP-interoperability floor; adding a capability here is a breaking
// change and must raise MinSupportedProtocolVersion accordingly.
var requiredProtocolCapabilities = []string{
	CapabilityStreamStartEnd,
	CapabilityBinaryBodyFrame,
	CapabilityStreamCancel,
}

// defaultProtocolCapabilities are every capability this build supports and
// advertises. New, optional capabilities are added here without being added
// to requiredProtocolCapabilities, so older peers that lack them keep
// working for everything except the features they gate.
var defaultProtocolCapabilities = append(append([]string(nil), requiredProtocolCapabilities...), CapabilityWebSocketTunnel)

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

// DefaultProtocolCapabilities returns every capability this build supports
// and advertises to a peer during authentication.
func DefaultProtocolCapabilities() []string {
	return append([]string(nil), defaultProtocolCapabilities...)
}

// RequiredProtocolCapabilities returns the capabilities a peer must declare
// to be considered compatible at all. It is the v0.2 HTTP-interoperability
// floor and intentionally excludes newer, additive capabilities such as
// CapabilityWebSocketTunnel.
func RequiredProtocolCapabilities() []string {
	return append([]string(nil), requiredProtocolCapabilities...)
}

// IsProtocolVersionSupported reports whether version is at or above
// MinSupportedProtocolVersion. It does not reject versions newer than
// ProtocolVersion: forward compatibility with a future peer is governed by
// capability negotiation, not by the version number.
func IsProtocolVersionSupported(version string) bool {
	major, minor, ok := parseProtocolVersion(version)
	if !ok {
		return false
	}
	minMajor, minMinor, _ := parseProtocolVersion(MinSupportedProtocolVersion)
	if major != minMajor {
		return major > minMajor
	}
	return minor >= minMinor
}

func parseProtocolVersion(version string) (major, minor int, ok bool) {
	parts := strings.SplitN(version, ".", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	major, errA := strconv.Atoi(parts[0])
	minor, errB := strconv.Atoi(parts[1])
	if errA != nil || errB != nil || major < 0 || minor < 0 {
		return 0, 0, false
	}
	return major, minor, true
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
