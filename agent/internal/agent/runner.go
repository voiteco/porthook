// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
)

const agentWebSocketPath = "/agent/connect"

type Runner struct {
	cfg        Config
	logger     *slog.Logger
	output     io.Writer
	httpClient *http.Client
	sendMu     sync.Mutex
	outputMu   sync.Mutex
}

func NewRunner(cfg Config, logger *slog.Logger, output io.Writer) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	if output == nil {
		output = io.Discard
	}
	cfg = normalizeConfig(cfg)

	return &Runner{
		cfg:    cfg,
		logger: logger,
		output: output,
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	wsURL, err := BuildWebSocketURL(r.cfg.ServerURL)
	if err != nil {
		return err
	}

	handshakeCtx, cancel := contextWithTimeout(ctx, r.cfg.HandshakeTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(handshakeCtx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to gateway %s: %w", wsURL, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := r.authenticate(handshakeCtx, conn); err != nil {
		return err
	}

	registered, err := r.registerTunnel(handshakeCtx, conn)
	if err != nil {
		return err
	}

	fmt.Fprintln(r.output, "Tunnel established")
	fmt.Fprintf(r.output, "Forwarding: %s -> %s\n", registered.PublicURL, r.cfg.LocalTarget)

	return r.serve(ctx, conn, registered.TunnelID)
}

func (r *Runner) authenticate(ctx context.Context, conn *websocket.Conn) error {
	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:        r.cfg.Token,
		AgentVersion: r.cfg.AgentVersion,
	})
	if err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		return fmt.Errorf("send auth request: %w", err)
	}

	var resp messages.Envelope
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	switch resp.Type {
	case messages.TypeAuthOK:
		return nil
	case messages.TypeAuthError:
		payload, err := messages.DecodePayload[messages.ErrorPayload](resp)
		if err != nil {
			return err
		}
		return formatAuthError(payload)
	default:
		return fmt.Errorf("unexpected auth response %s", resp.Type)
	}
}

func (r *Runner) registerTunnel(ctx context.Context, conn *websocket.Conn) (messages.TunnelRegistered, error) {
	registration, err := messages.New(messages.TypeTunnelRegister, messages.TunnelRegister{
		Protocol:           "http",
		LocalTarget:        r.cfg.LocalTarget,
		RequestedSubdomain: r.cfg.RequestedSubdomain,
	})
	if err != nil {
		return messages.TunnelRegistered{}, err
	}
	if err := wsjson.Write(ctx, conn, registration); err != nil {
		return messages.TunnelRegistered{}, fmt.Errorf("send tunnel registration: %w", err)
	}

	var resp messages.Envelope
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		return messages.TunnelRegistered{}, fmt.Errorf("read tunnel registration: %w", err)
	}
	switch resp.Type {
	case messages.TypeTunnelRegistered:
		return messages.DecodePayload[messages.TunnelRegistered](resp)
	case messages.TypeTunnelError:
		payload, err := messages.DecodePayload[messages.ErrorPayload](resp)
		if err != nil {
			return messages.TunnelRegistered{}, err
		}
		return messages.TunnelRegistered{}, formatTunnelRegistrationError(payload)
	default:
		return messages.TunnelRegistered{}, fmt.Errorf("unexpected tunnel registration response %s", resp.Type)
	}
}

func formatAuthError(payload messages.ErrorPayload) error {
	message := errorPayloadMessage(payload)
	if payload.Code == "invalid_token" {
		return fmt.Errorf("authentication failed: invalid token; check --token or PORTHOOK_TOKEN")
	}
	return fmt.Errorf("authentication failed: %s", message)
}

func formatTunnelRegistrationError(payload messages.ErrorPayload) error {
	return fmt.Errorf("tunnel registration failed: %s", errorPayloadMessage(payload))
}

func errorPayloadMessage(payload messages.ErrorPayload) string {
	message := strings.TrimSpace(payload.Message)
	if message != "" {
		return message
	}
	if payload.Code != "" {
		return payload.Code
	}
	return "unknown error"
}

func (r *Runner) serve(ctx context.Context, conn *websocket.Conn, tunnelID string) error {
	for {
		var env messages.Envelope
		if err := wsjson.Read(ctx, conn, &env); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if status := websocket.CloseStatus(err); status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
				return nil
			}
			return fmt.Errorf("read gateway message: %w", err)
		}

		switch env.Type {
		case messages.TypeHTTPRequest:
			go func(env messages.Envelope) {
				if err := r.handleHTTPRequest(ctx, conn, tunnelID, env); err != nil {
					r.logger.Warn("http request failed", "stream_id", env.StreamID, "error", err)
				}
			}(env)
		case messages.TypePing:
			pong, err := messages.New(messages.TypePong, nil)
			if err != nil {
				return err
			}
			if err := r.write(ctx, conn, pong); err != nil {
				return fmt.Errorf("write pong: %w", err)
			}
		default:
			r.logger.Warn("unexpected gateway message", "type", env.Type)
		}
	}
}

func (r *Runner) handleHTTPRequest(ctx context.Context, conn *websocket.Conn, tunnelID string, env messages.Envelope) error {
	req, err := messages.DecodePayload[httpwire.Request](env)
	if err != nil {
		return err
	}

	started := time.Now()
	resp, err := ForwardHTTPRequest(ctx, r.httpClient, r.cfg.LocalTarget, req, r.cfg.MaxResponseBodyBytes)
	if err != nil {
		streamErr, buildErr := messages.NewStream(messages.TypeHTTPStreamError, env.StreamID, tunnelID, messages.ErrorPayload{
			Code:    "local_request_failed",
			Message: err.Error(),
		})
		if buildErr != nil {
			return buildErr
		}
		if writeErr := r.write(ctx, conn, streamErr); writeErr != nil {
			return fmt.Errorf("write stream error: %w", writeErr)
		}
		return err
	}

	response, err := messages.NewStream(messages.TypeHTTPResponse, env.StreamID, tunnelID, resp)
	if err != nil {
		return err
	}
	if err := r.write(ctx, conn, response); err != nil {
		return fmt.Errorf("write http response: %w", err)
	}

	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	fmt.Fprintf(r.output, "%s %s -> %d %dms\n", req.Method, requestDisplayPath(req.Path, req.Query), resp.Status, time.Since(started).Milliseconds())
	return nil
}

func (r *Runner) write(ctx context.Context, conn *websocket.Conn, env messages.Envelope) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	writeCtx, cancel := contextWithTimeout(ctx, r.cfg.WebSocketWriteTimeout)
	defer cancel()
	return wsjson.Write(writeCtx, conn, env)
}

func contextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func BuildWebSocketURL(serverURL string) (string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("server URL must use http, https, ws, or wss")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("server URL must include host")
	}

	parsed.Path = joinURLPath(parsed.Path, agentWebSocketPath)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func requestDisplayPath(path, query string) string {
	if strings.TrimSpace(query) == "" {
		return path
	}
	return path + "?" + query
}
