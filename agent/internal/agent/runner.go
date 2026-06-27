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
}

func NewRunner(cfg Config, logger *slog.Logger, output io.Writer) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	if output == nil {
		output = io.Discard
	}

	timeout := cfg.RequestTimeout
	if timeout == 0 {
		timeout = defaultRequestTimeout
	}

	return &Runner{
		cfg:    cfg,
		logger: logger,
		output: output,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	wsURL, err := BuildWebSocketURL(r.cfg.ServerURL)
	if err != nil {
		return err
	}

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to gateway: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := r.authenticate(ctx, conn); err != nil {
		return err
	}

	registered, err := r.registerTunnel(ctx, conn)
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
		return fmt.Errorf("authentication failed: %s", payload.Message)
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
		return messages.TunnelRegistered{}, fmt.Errorf("tunnel registration failed: %s", payload.Message)
	default:
		return messages.TunnelRegistered{}, fmt.Errorf("unexpected tunnel registration response %s", resp.Type)
	}
}

func (r *Runner) serve(ctx context.Context, conn *websocket.Conn, tunnelID string) error {
	for {
		var env messages.Envelope
		if err := wsjson.Read(ctx, conn, &env); err != nil {
			return fmt.Errorf("read gateway message: %w", err)
		}

		switch env.Type {
		case messages.TypeHTTPRequest:
			if err := r.handleHTTPRequest(ctx, conn, tunnelID, env); err != nil {
				r.logger.Warn("http request failed", "stream_id", env.StreamID, "error", err)
			}
		case messages.TypePing:
			pong, err := messages.New(messages.TypePong, nil)
			if err != nil {
				return err
			}
			if err := wsjson.Write(ctx, conn, pong); err != nil {
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
	resp, err := ForwardHTTPRequest(ctx, r.httpClient, r.cfg.LocalTarget, req)
	if err != nil {
		streamErr, buildErr := messages.NewStream(messages.TypeHTTPStreamError, env.StreamID, tunnelID, messages.ErrorPayload{
			Code:    "local_request_failed",
			Message: err.Error(),
		})
		if buildErr != nil {
			return buildErr
		}
		if writeErr := wsjson.Write(ctx, conn, streamErr); writeErr != nil {
			return fmt.Errorf("write stream error: %w", writeErr)
		}
		return err
	}

	response, err := messages.NewStream(messages.TypeHTTPResponse, env.StreamID, tunnelID, resp)
	if err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, response); err != nil {
		return fmt.Errorf("write http response: %w", err)
	}

	fmt.Fprintf(r.output, "%s %s -> %d %dms\n", req.Method, req.Path, resp.Status, time.Since(started).Milliseconds())
	return nil
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
