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
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wswire"
)

const agentWebSocketPath = "/agent/connect"

type Runner struct {
	cfg        Config
	logger     *slog.Logger
	output     io.Writer
	httpClient *http.Client
	sendMu     sync.Mutex
	outputMu   sync.Mutex

	activeMu      sync.Mutex
	activeStreams map[string]*activeStream
}

type activeStream struct {
	cancel   context.CancelFunc
	chunks   chan requestBodyChunk
	done     chan struct{}
	doneOnce sync.Once
}

type requestBodyChunk struct {
	data []byte
	end  bool
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
		activeStreams: make(map[string]*activeStream),
	}
}

func (r *Runner) Run(ctx context.Context) error {
	wsURL, err := BuildWebSocketURL(r.cfg.ServerURL)
	if err != nil {
		return err
	}

	attempt := 0
	connectedBefore := false
	for {
		connected, err := r.runOnce(ctx, wsURL, connectedBefore)
		if connected {
			connectedBefore = true
			attempt = 0
		}
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		if isPermanent(err) {
			return err
		}

		delay := reconnectDelay(attempt, r.cfg.ReconnectInitialDelay, r.cfg.ReconnectMaxDelay, r.cfg.ReconnectJitter, randomReconnectJitter)
		attempt++
		r.logger.Warn("gateway connection lost", "error", err, "reconnect_delay_ms", delay.Milliseconds())
		r.writeReconnectOutput(err, delay, connectedBefore)
		if err := waitForReconnect(ctx, delay); err != nil {
			return nil
		}
	}
}

func (r *Runner) runOnce(ctx context.Context, wsURL string, restored bool) (bool, error) {
	handshakeCtx, cancel := contextWithTimeout(ctx, r.cfg.HandshakeTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(handshakeCtx, wsURL, nil)
	if err != nil {
		return false, fmt.Errorf("connect to gateway %s: %w", wsURL, err)
	}
	conn.SetReadLimit(wswire.ReadLimitForChunkBytes(r.cfg.StreamChunkBytes))
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := r.authenticate(handshakeCtx, conn); err != nil {
		return false, err
	}

	registered, err := r.registerTunnel(handshakeCtx, conn)
	if err != nil {
		return false, err
	}

	r.writeTunnelReadyOutput(restored, registered.PublicURL)

	sessionCtx, stopSession := context.WithCancel(ctx)
	defer stopSession()

	stopKeepalive := r.startKeepalive(sessionCtx, conn, registered.TunnelID)
	defer stopKeepalive()

	return true, r.serve(sessionCtx, conn, registered.TunnelID)
}

func (r *Runner) authenticate(ctx context.Context, conn *websocket.Conn) error {
	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           r.cfg.Token,
		AgentVersion:    r.cfg.AgentVersion,
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
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
		payload, err := messages.DecodePayload[messages.AuthOK](resp)
		if err != nil {
			return permanent(err)
		}
		if err := r.validateAuthOK(payload); err != nil {
			return permanent(err)
		}
		return nil
	case messages.TypeAuthError:
		payload, err := messages.DecodePayload[messages.ErrorPayload](resp)
		if err != nil {
			return permanent(err)
		}
		return permanent(formatAuthError(payload))
	default:
		return permanent(fmt.Errorf("unexpected auth response %s", resp.Type))
	}
}

func (r *Runner) validateAuthOK(payload messages.AuthOK) error {
	if payload.ProtocolVersion != messages.ProtocolVersion {
		return fmt.Errorf("gateway protocol version %q is not supported, expected %q", payload.ProtocolVersion, messages.ProtocolVersion)
	}

	missing := messages.MissingRequiredCapabilities(payload.Capabilities, messages.DefaultProtocolCapabilities())
	if len(missing) > 0 {
		return fmt.Errorf("gateway protocol is incompatible: missing capabilities: %s", strings.Join(missing, ", "))
	}

	return nil
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
			return messages.TunnelRegistered{}, permanent(err)
		}
		return messages.TunnelRegistered{}, permanent(formatTunnelRegistrationError(payload))
	default:
		return messages.TunnelRegistered{}, permanent(fmt.Errorf("unexpected tunnel registration response %s", resp.Type))
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
		msg, err := wswire.Read(ctx, conn)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if status := websocket.CloseStatus(err); status == websocket.StatusNormalClosure {
				return nil
			}
			return fmt.Errorf("read gateway message: %w", err)
		}
		env := msg.Envelope

		switch env.Type {
		case messages.TypeHTTPRequest:
			streamCtx, cancelStream := context.WithCancel(ctx)
			r.addActiveStream(env.StreamID, newActiveStream(cancelStream, nil))
			go func(env messages.Envelope) {
				defer r.removeActiveStream(env.StreamID)
				defer cancelStream()
				_ = r.handleHTTPRequest(streamCtx, conn, tunnelID, env)
			}(env)
		case messages.TypeHTTPRequestStart:
			req, err := messages.DecodePayload[httpwire.RequestStart](env)
			if err != nil {
				r.logger.Warn("decode request start failed", "stream_id", env.StreamID, "error", err)
				continue
			}
			streamCtx, cancelStream := context.WithCancel(ctx)
			stream := newActiveStream(cancelStream, make(chan requestBodyChunk, 32))
			r.addActiveStream(env.StreamID, stream)
			go func(env messages.Envelope, req httpwire.RequestStart, stream *activeStream) {
				defer r.removeActiveStream(env.StreamID)
				defer cancelStream()
				_ = r.handleHTTPStream(streamCtx, conn, tunnelID, env, req, stream.chunks)
			}(env, req, stream)
		case messages.TypeHTTPRequestBody:
			var data []byte
			if msg.BinaryBody {
				data = msg.Body
			} else {
				payload, err := messages.DecodePayload[httpwire.BodyChunk](env)
				if err != nil {
					r.logger.Warn("decode request body failed", "stream_id", env.StreamID, "error", err)
					continue
				}
				data = payload.Data
			}
			if len(data) > r.cfg.StreamChunkBytes {
				r.logger.Warn("request body chunk too large", "stream_id", env.StreamID, "chunk_bytes", len(data))
				r.cancelActiveStream(env.StreamID)
				continue
			}
			if err := r.deliverActiveStreamChunk(ctx, env.StreamID, requestBodyChunk{data: data}); err != nil {
				r.logger.Warn("deliver request body failed", "stream_id", env.StreamID, "error", err)
			}
		case messages.TypeHTTPRequestEnd:
			if err := r.deliverActiveStreamChunk(ctx, env.StreamID, requestBodyChunk{end: true}); err != nil {
				r.logger.Warn("deliver request end failed", "stream_id", env.StreamID, "error", err)
			}
		case messages.TypeHTTPStreamCancel:
			payload, err := messages.DecodePayload[messages.StreamCancel](env)
			if err != nil {
				r.logger.Warn("decode stream cancel failed", "stream_id", env.StreamID, "error", err)
				continue
			}
			if r.cancelActiveStream(env.StreamID) {
				r.logger.Info("stream canceled by gateway", "stream_id", env.StreamID, "reason", payload.Reason)
			}
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

func (r *Runner) writeTunnelReadyOutput(restored bool, publicURL string) {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()

	if restored {
		fmt.Fprintln(r.output, "Tunnel restored")
	} else {
		fmt.Fprintln(r.output, "Tunnel established")
	}
	fmt.Fprintf(r.output, "Forwarding: %s -> %s\n", publicURL, r.cfg.LocalTarget)
}

func (r *Runner) writeReconnectOutput(err error, delay time.Duration, disconnected bool) {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()

	if disconnected {
		fmt.Fprintf(r.output, "Disconnected, reconnecting in %s: %s\n", delay.Round(time.Millisecond), err)
		return
	}
	fmt.Fprintf(r.output, "Connection failed, retrying in %s: %s\n", delay.Round(time.Millisecond), err)
}

func waitForReconnect(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Runner) handleHTTPRequest(ctx context.Context, conn *websocket.Conn, tunnelID string, env messages.Envelope) (err error) {
	started := time.Now()
	status := 0
	outcome := "unknown"
	var req httpwire.Request
	var responseBytes int64
	defer func() {
		r.logLocalRequest(env.StreamID, tunnelID, req, status, outcome, responseBytes, time.Since(started), err)
	}()

	req, err = messages.DecodePayload[httpwire.Request](env)
	if err != nil {
		outcome = "decode_failed"
		return err
	}

	resp, err := ForwardHTTPRequest(ctx, r.httpClient, r.cfg.LocalTarget, req, r.cfg.MaxResponseBodyBytes)
	if err != nil {
		if ctx.Err() != nil {
			outcome = "stream_canceled"
			r.writeRequestOutput(req, 0, time.Since(started), ctx.Err())
			return ctx.Err()
		}
		outcome = "local_request_failed"
		streamErr, buildErr := messages.NewStream(messages.TypeHTTPStreamError, env.StreamID, tunnelID, messages.ErrorPayload{
			Code:    "local_request_failed",
			Message: err.Error(),
		})
		if buildErr != nil {
			outcome = "stream_error_build_failed"
			return buildErr
		}
		if writeErr := r.write(ctx, conn, streamErr); writeErr != nil {
			outcome = "stream_error_write_failed"
			return fmt.Errorf("write stream error: %w", writeErr)
		}
		r.writeRequestOutput(req, 0, time.Since(started), err)
		return err
	}
	status = resp.Status
	responseBytes = int64(len(resp.Body))

	response, err := messages.NewStream(messages.TypeHTTPResponse, env.StreamID, tunnelID, resp)
	if err != nil {
		outcome = "response_build_failed"
		return err
	}
	if err := r.write(ctx, conn, response); err != nil {
		outcome = "response_write_failed"
		return fmt.Errorf("write http response: %w", err)
	}
	outcome = "completed"

	r.writeRequestOutput(req, resp.Status, time.Since(started), nil)
	return nil
}

func (r *Runner) handleHTTPStream(
	ctx context.Context,
	conn *websocket.Conn,
	tunnelID string,
	env messages.Envelope,
	req httpwire.RequestStart,
	chunks <-chan requestBodyChunk,
) (err error) {
	started := time.Now()
	status := 0
	outcome := "unknown"
	var responseBytes int64

	body, requestBytes := requestBodyFromChunks(ctx, chunks)
	defer body.Close()
	defer func() {
		r.logLocalRequestFields(
			env.StreamID,
			tunnelID,
			req.Method,
			req.Path,
			req.Query,
			status,
			outcome,
			requestBytes.Load(),
			responseBytes,
			time.Since(started),
			err,
		)
	}()

	writeResponseStart := func(resp httpwire.ResponseStart) error {
		if resp.Status <= 0 {
			resp.Status = http.StatusBadGateway
		}
		status = resp.Status
		start, err := messages.NewStream(messages.TypeHTTPResponseStart, env.StreamID, tunnelID, resp)
		if err != nil {
			return err
		}
		return r.write(ctx, conn, start)
	}
	writeResponseBody := func(chunk []byte) error {
		return r.writeBinaryBody(ctx, conn, messages.TypeHTTPResponseBody, env.StreamID, tunnelID, chunk)
	}

	status, responseBytes, err = ForwardHTTPStream(
		ctx,
		r.httpClient,
		r.cfg.LocalTarget,
		req,
		body,
		r.cfg.MaxResponseBodyBytes,
		r.cfg.StreamChunkBytes,
		writeResponseStart,
		writeResponseBody,
	)
	if err != nil {
		if ctx.Err() != nil {
			outcome = "stream_canceled"
			r.writeStreamRequestOutput(req, 0, time.Since(started), ctx.Err())
			return ctx.Err()
		}
		outcome = "local_request_failed"
		streamErr, buildErr := messages.NewStream(messages.TypeHTTPStreamError, env.StreamID, tunnelID, messages.ErrorPayload{
			Code:    "local_request_failed",
			Message: err.Error(),
		})
		if buildErr != nil {
			outcome = "stream_error_build_failed"
			return buildErr
		}
		if writeErr := r.write(ctx, conn, streamErr); writeErr != nil {
			outcome = "stream_error_write_failed"
			return fmt.Errorf("write stream error: %w", writeErr)
		}
		r.writeStreamRequestOutput(req, 0, time.Since(started), err)
		return err
	}

	end, err := messages.NewStream(messages.TypeHTTPResponseEnd, env.StreamID, tunnelID, nil)
	if err != nil {
		outcome = "response_end_build_failed"
		return err
	}
	if err := r.write(ctx, conn, end); err != nil {
		outcome = "response_end_write_failed"
		return fmt.Errorf("write response end: %w", err)
	}
	outcome = "completed"

	r.writeStreamRequestOutput(req, status, time.Since(started), nil)
	return nil
}

func requestBodyFromChunks(ctx context.Context, chunks <-chan requestBodyChunk) (io.ReadCloser, *atomic.Int64) {
	reader, writer := io.Pipe()
	var requestBytes atomic.Int64

	go func() {
		for {
			select {
			case <-ctx.Done():
				_ = writer.CloseWithError(ctx.Err())
				return
			case chunk := <-chunks:
				if chunk.end {
					_ = writer.Close()
					return
				}
				if len(chunk.data) == 0 {
					continue
				}
				requestBytes.Add(int64(len(chunk.data)))
				if _, err := writer.Write(chunk.data); err != nil {
					return
				}
			}
		}
	}()

	return reader, &requestBytes
}

func newActiveStream(cancel context.CancelFunc, chunks chan requestBodyChunk) *activeStream {
	return &activeStream{
		cancel: cancel,
		chunks: chunks,
		done:   make(chan struct{}),
	}
}

func (s *activeStream) closeDone() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (r *Runner) addActiveStream(streamID string, stream *activeStream) {
	if streamID == "" {
		return
	}
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	r.activeStreams[streamID] = stream
}

func (r *Runner) cancelActiveStream(streamID string) bool {
	r.activeMu.Lock()
	stream, ok := r.activeStreams[streamID]
	r.activeMu.Unlock()
	if !ok {
		return false
	}
	stream.cancel()
	stream.closeDone()
	return true
}

func (r *Runner) deliverActiveStreamChunk(ctx context.Context, streamID string, chunk requestBodyChunk) error {
	r.activeMu.Lock()
	stream, ok := r.activeStreams[streamID]
	r.activeMu.Unlock()
	if !ok || stream.chunks == nil {
		return fmt.Errorf("unknown stream")
	}

	select {
	case stream.chunks <- chunk:
		return nil
	case <-stream.done:
		return fmt.Errorf("stream closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) removeActiveStream(streamID string) {
	if streamID == "" {
		return
	}
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	stream, ok := r.activeStreams[streamID]
	if ok {
		stream.closeDone()
		delete(r.activeStreams, streamID)
	}
}

func (r *Runner) logLocalRequest(streamID, tunnelID string, req httpwire.Request, status int, outcome string, responseBytes int64, duration time.Duration, err error) {
	r.logLocalRequestFields(
		streamID,
		tunnelID,
		req.Method,
		req.Path,
		req.Query,
		status,
		outcome,
		int64(len(req.Body)),
		responseBytes,
		duration,
		err,
	)
}

func (r *Runner) logLocalRequestFields(streamID, tunnelID, method, path, query string, status int, outcome string, requestBytes, responseBytes int64, duration time.Duration, err error) {
	level := slog.LevelInfo
	if err != nil || status >= 500 {
		level = slog.LevelWarn
	}

	attrs := []slog.Attr{
		slog.String("stream_id", streamID),
		slog.String("tunnel_id", tunnelID),
		slog.String("method", method),
		slog.String("path", requestDisplayPath(path, query)),
		slog.Int("status", status),
		slog.String("outcome", outcome),
		slog.Int64("request_bytes", requestBytes),
		slog.Int64("response_bytes", responseBytes),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}

	r.logger.LogAttrs(context.Background(), level, "local request", attrs...)
}

func (r *Runner) writeRequestOutput(req httpwire.Request, status int, duration time.Duration, err error) {
	r.writeRequestOutputFields(req.Method, req.Path, req.Query, status, duration, err)
}

func (r *Runner) writeStreamRequestOutput(req httpwire.RequestStart, status int, duration time.Duration, err error) {
	r.writeRequestOutputFields(req.Method, req.Path, req.Query, status, duration, err)
}

func (r *Runner) writeRequestOutputFields(method, path, query string, status int, duration time.Duration, err error) {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()

	if err != nil {
		fmt.Fprintf(r.output, "%s %s -> error %dms: %s\n", method, requestDisplayPath(path, query), duration.Milliseconds(), err)
		return
	}
	fmt.Fprintf(r.output, "%s %s -> %d %dms\n", method, requestDisplayPath(path, query), status, duration.Milliseconds())
}

func (r *Runner) write(ctx context.Context, conn *websocket.Conn, env messages.Envelope) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	writeCtx, cancel := contextWithTimeout(ctx, r.cfg.WebSocketWriteTimeout)
	defer cancel()
	return wswire.WriteEnvelope(writeCtx, conn, env)
}

func (r *Runner) writeBinaryBody(ctx context.Context, conn *websocket.Conn, typ messages.Type, streamID, tunnelID string, body []byte) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	writeCtx, cancel := contextWithTimeout(ctx, r.cfg.WebSocketWriteTimeout)
	defer cancel()
	return wswire.WriteBinaryBody(writeCtx, conn, typ, streamID, tunnelID, body)
}

func (r *Runner) ping(ctx context.Context, conn *websocket.Conn) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	pingCtx, cancel := contextWithTimeout(ctx, r.cfg.WebSocketPongTimeout)
	defer cancel()
	return conn.Ping(pingCtx)
}

func (r *Runner) startKeepalive(ctx context.Context, conn *websocket.Conn, tunnelID string) func() {
	if r.cfg.WebSocketPingInterval <= 0 {
		return func() {}
	}

	keepaliveCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(r.cfg.WebSocketPingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-keepaliveCtx.Done():
				return
			case <-ticker.C:
				if err := r.ping(keepaliveCtx, conn); err != nil {
					r.logger.Warn("gateway websocket keepalive failed", "tunnel_id", tunnelID, "error", err)
					_ = conn.Close(websocket.StatusGoingAway, "websocket keepalive failed")
					return
				}
			}
		}
	}()

	return cancel
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
