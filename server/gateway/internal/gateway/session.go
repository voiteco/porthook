// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wswire"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
)

var ErrStreamLimitExceeded = errors.New("tunnel stream limit exceeded")
var ErrRequestBodyTooLarge = errors.New("request body too large")

type streamRoundTripResult struct {
	Status        int
	RequestBytes  int64
	ResponseBytes int64
}

type agentSession struct {
	conn     *websocket.Conn
	tunnel   *registry.Session
	sendMu   sync.Mutex
	writeTO  time.Duration
	done     chan struct{}
	doneOnce sync.Once
	streams  chan struct{}
	limiter  *rateLimiter

	pendingMu sync.RWMutex
	pending   map[string]chan wswire.Message
}

func newAgentSession(conn *websocket.Conn, tunnel *registry.Session, writeTimeout time.Duration, maxConcurrentStreams, rateLimitRPS, rateLimitBurst int) *agentSession {
	var streams chan struct{}
	if maxConcurrentStreams > 0 {
		streams = make(chan struct{}, maxConcurrentStreams)
	}
	return &agentSession{
		conn:    conn,
		tunnel:  tunnel,
		writeTO: writeTimeout,
		done:    make(chan struct{}),
		streams: streams,
		limiter: newRateLimiter(rateLimitRPS, rateLimitBurst, time.Now()),
		pending: make(map[string]chan wswire.Message),
	}
}

func (s *agentSession) allowRequest(now time.Time) bool {
	return s.limiter.allow(now)
}

func (s *agentSession) roundTrip(ctx context.Context, streamID string, req httpwire.Request) (httpwire.Response, error) {
	if err := s.acquireStream(); err != nil {
		return httpwire.Response{}, err
	}
	defer s.releaseStream()

	ch := make(chan wswire.Message, 1)
	s.addPending(streamID, ch)
	defer s.removePending(streamID)

	env, err := messages.NewStream(messages.TypeHTTPRequest, streamID, s.tunnel.TunnelID, req)
	if err != nil {
		return httpwire.Response{}, err
	}
	if err := s.write(ctx, env); err != nil {
		return httpwire.Response{}, fmt.Errorf("write tunnel request: %w", err)
	}

	select {
	case <-ctx.Done():
		_ = s.cancelStream(context.Background(), streamID, streamCancelReason(ctx.Err()))
		return httpwire.Response{}, ctx.Err()
	case <-s.done:
		return httpwire.Response{}, errors.New("agent disconnected")
	case respMsg := <-ch:
		respEnv := respMsg.Envelope
		switch respEnv.Type {
		case messages.TypeHTTPResponse:
			return messages.DecodePayload[httpwire.Response](respEnv)
		case messages.TypeHTTPStreamError:
			payload, err := messages.DecodePayload[messages.ErrorPayload](respEnv)
			if err != nil {
				return httpwire.Response{}, err
			}
			return httpwire.Response{}, errors.New(payload.Message)
		default:
			return httpwire.Response{}, fmt.Errorf("unexpected response message %s", respEnv.Type)
		}
	}
}

func (s *agentSession) streamRoundTrip(
	ctx context.Context,
	streamID string,
	req httpwire.RequestStart,
	body io.Reader,
	maxBodyBytes int64,
	chunkBytes int,
	writeResponseStart func(httpwire.ResponseStart),
	writeResponseBody func([]byte) (int, error),
) (streamRoundTripResult, error) {
	if err := s.acquireStream(); err != nil {
		return streamRoundTripResult{}, err
	}
	defer s.releaseStream()

	ch := make(chan wswire.Message, 32)
	s.addPending(streamID, ch)
	defer s.removePending(streamID)

	start, err := messages.NewStream(messages.TypeHTTPRequestStart, streamID, s.tunnel.TunnelID, req)
	if err != nil {
		return streamRoundTripResult{}, err
	}
	if err := s.write(ctx, start); err != nil {
		return streamRoundTripResult{}, fmt.Errorf("write request start: %w", err)
	}

	result := streamRoundTripResult{}
	result.RequestBytes, err = s.writeRequestBody(ctx, streamID, body, maxBodyBytes, chunkBytes)
	if err != nil {
		_ = s.cancelStream(context.Background(), streamID, streamCancelReasonForError(err))
		return result, err
	}

	end, err := messages.NewStream(messages.TypeHTTPRequestEnd, streamID, s.tunnel.TunnelID, nil)
	if err != nil {
		return result, err
	}
	if err := s.write(ctx, end); err != nil {
		return result, fmt.Errorf("write request end: %w", err)
	}

	result, err = s.readStreamResponse(ctx, streamID, ch, chunkBytes, writeResponseStart, writeResponseBody, result)
	if err != nil && !isStreamContextDoneCause(err) {
		_ = s.cancelStream(context.Background(), streamID, streamCancelReasonForError(err))
	}
	return result, err
}

// isStreamContextDoneCause reports whether err is a cause readStreamResponse
// already sent a stream-cancel message for when its ctx.Done() branch fired,
// so streamRoundTrip does not send a second, redundant cancellation.
func isStreamContextDoneCause(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrStreamRequestTimeout) ||
		errors.Is(err, ErrStreamIdleTimeout) ||
		errors.Is(err, ErrStreamMaxLifetimeExceeded)
}

func (s *agentSession) writeRequestBody(ctx context.Context, streamID string, body io.Reader, maxBodyBytes int64, chunkBytes int) (int64, error) {
	if body == nil {
		return 0, nil
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	if chunkBytes <= 0 {
		chunkBytes = defaultStreamChunkBytes
	}

	buf := make([]byte, chunkBytes)
	var sent int64
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			sent += int64(n)
			if sent > maxBodyBytes {
				return sent, ErrRequestBodyTooLarge
			}
			chunk := append([]byte(nil), buf[:n]...)
			if err := s.writeBinaryBody(ctx, messages.TypeHTTPRequestBody, streamID, chunk); err != nil {
				return sent, fmt.Errorf("write request body: %w", err)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return sent, nil
			}
			return sent, fmt.Errorf("read public request body: %w", readErr)
		}
	}
}

func (s *agentSession) readStreamResponse(
	ctx context.Context,
	streamID string,
	ch <-chan wswire.Message,
	chunkBytes int,
	writeResponseStart func(httpwire.ResponseStart),
	writeResponseBody func([]byte) (int, error),
	result streamRoundTripResult,
) (streamRoundTripResult, error) {
	if chunkBytes <= 0 {
		chunkBytes = defaultStreamChunkBytes
	}

	responseStarted := false
	for {
		select {
		case <-ctx.Done():
			cause := context.Cause(ctx)
			_ = s.cancelStream(context.Background(), streamID, streamCancelReason(cause))
			return result, cause
		case <-s.done:
			return result, errors.New("agent disconnected")
		case respMsg := <-ch:
			respEnv := respMsg.Envelope
			switch respEnv.Type {
			case messages.TypeHTTPResponseStart:
				resp, err := messages.DecodePayload[httpwire.ResponseStart](respEnv)
				if err != nil {
					return result, err
				}
				if resp.Status <= 0 {
					resp.Status = http.StatusBadGateway
				}
				result.Status = resp.Status
				responseStarted = true
				writeResponseStart(resp)
			case messages.TypeHTTPResponseBody:
				if !responseStarted {
					return result, errors.New("response body received before response start")
				}
				chunk, err := responseBodyChunk(respMsg, chunkBytes)
				if err != nil {
					return result, err
				}
				n, err := writeResponseBody(chunk)
				result.ResponseBytes += int64(n)
				if err != nil {
					return result, err
				}
				if n != len(chunk) {
					return result, io.ErrShortWrite
				}
			case messages.TypeHTTPResponseEnd:
				if !responseStarted {
					resp := httpwire.ResponseStart{Status: http.StatusOK}
					result.Status = resp.Status
					writeResponseStart(resp)
				}
				return result, nil
			case messages.TypeHTTPResponse:
				return s.handleWholeResponse(respEnv, writeResponseStart, writeResponseBody, result)
			case messages.TypeHTTPStreamError:
				payload, err := messages.DecodePayload[messages.ErrorPayload](respEnv)
				if err != nil {
					return result, err
				}
				return result, errors.New(payload.Message)
			default:
				return result, fmt.Errorf("unexpected response message %s", respEnv.Type)
			}
		}
	}
}

func responseBodyChunk(msg wswire.Message, chunkBytes int) ([]byte, error) {
	var chunk []byte
	if msg.BinaryBody {
		chunk = msg.Body
	} else {
		payload, err := messages.DecodePayload[httpwire.BodyChunk](msg.Envelope)
		if err != nil {
			return nil, err
		}
		chunk = payload.Data
	}
	if len(chunk) > chunkBytes {
		return nil, fmt.Errorf("response chunk exceeds %d bytes", chunkBytes)
	}
	return chunk, nil
}

func (s *agentSession) handleWholeResponse(
	respEnv messages.Envelope,
	writeResponseStart func(httpwire.ResponseStart),
	writeResponseBody func([]byte) (int, error),
	result streamRoundTripResult,
) (streamRoundTripResult, error) {
	resp, err := messages.DecodePayload[httpwire.Response](respEnv)
	if err != nil {
		return result, err
	}
	if resp.Status <= 0 {
		resp.Status = http.StatusBadGateway
	}
	result.Status = resp.Status
	writeResponseStart(httpwire.ResponseStart{
		Status: resp.Status,
		Header: resp.Header,
	})
	if len(resp.Body) == 0 {
		return result, nil
	}
	n, err := writeResponseBody(resp.Body)
	result.ResponseBytes += int64(n)
	if err != nil {
		return result, err
	}
	if n != len(resp.Body) {
		return result, io.ErrShortWrite
	}
	return result, nil
}

func (s *agentSession) acquireStream() error {
	select {
	case <-s.done:
		return errors.New("agent disconnected")
	default:
	}

	if s.streams == nil {
		return nil
	}

	select {
	case s.streams <- struct{}{}:
		return nil
	default:
		return ErrStreamLimitExceeded
	}
}

func (s *agentSession) releaseStream() {
	if s.streams == nil {
		return
	}
	select {
	case <-s.streams:
	default:
	}
}

func (s *agentSession) activeStreams() int {
	if s.streams == nil {
		return 0
	}
	return len(s.streams)
}

func (s *agentSession) streamCapacity() int {
	if s.streams == nil {
		return 0
	}
	return cap(s.streams)
}

func (s *agentSession) cancelStream(ctx context.Context, streamID, reason string) error {
	env, err := messages.NewStream(messages.TypeHTTPStreamCancel, streamID, s.tunnel.TunnelID, messages.StreamCancel{
		Reason: reason,
	})
	if err != nil {
		return err
	}
	return s.write(ctx, env)
}

func streamCancelReason(err error) string {
	switch {
	case errors.Is(err, ErrStreamRequestTimeout):
		return "gateway timed out waiting for a response"
	case errors.Is(err, ErrStreamIdleTimeout):
		return "gateway stream idle timeout"
	case errors.Is(err, ErrStreamMaxLifetimeExceeded):
		return "gateway stream exceeded its maximum lifetime"
	case errors.Is(err, context.DeadlineExceeded):
		return "gateway stream timeout"
	case errors.Is(err, context.Canceled):
		return "public request canceled"
	default:
		return "gateway stream canceled"
	}
}

func streamCancelReasonForError(err error) string {
	switch {
	case errors.Is(err, ErrRequestBodyTooLarge):
		return "public request body too large"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return streamCancelReason(err)
	default:
		return "gateway stream failed"
	}
}

func (s *agentSession) write(ctx context.Context, env messages.Envelope) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	writeCtx, cancel := contextWithTimeout(ctx, s.writeTO)
	defer cancel()
	return wswire.WriteEnvelope(writeCtx, s.conn, env)
}

func (s *agentSession) writeBinaryBody(ctx context.Context, typ messages.Type, streamID string, body []byte) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	writeCtx, cancel := contextWithTimeout(ctx, s.writeTO)
	defer cancel()
	return wswire.WriteBinaryBody(writeCtx, s.conn, typ, streamID, s.tunnel.TunnelID, body)
}

func (s *agentSession) ping(ctx context.Context, timeout time.Duration) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	pingCtx, cancel := contextWithTimeout(ctx, timeout)
	defer cancel()
	return s.conn.Ping(pingCtx)
}

func (s *agentSession) startKeepalive(ctx context.Context, interval, timeout time.Duration, logger *slog.Logger) func() {
	if interval <= 0 {
		return func() {}
	}

	keepaliveCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-keepaliveCtx.Done():
				return
			case <-ticker.C:
				if err := s.ping(keepaliveCtx, timeout); err != nil {
					logger.Warn("agent websocket keepalive failed",
						"event", "gateway.agent_keepalive_failed",
						"tunnel_id", s.tunnel.TunnelID,
						"subdomain", s.tunnel.Subdomain,
						"error", err,
					)
					_ = s.close(websocket.StatusGoingAway, "websocket keepalive failed")
					return
				}
			}
		}
	}()

	return cancel
}

func (s *agentSession) readLoop(ctx context.Context, logger *slog.Logger) {
	defer s.closeDone()

	for {
		msg, err := wswire.Read(ctx, s.conn)
		if err != nil {
			logger.Info("agent read loop stopped", "event", "gateway.agent_read_loop_stopped", "tunnel_id", s.tunnel.TunnelID, "subdomain", s.tunnel.Subdomain, "error", err)
			return
		}
		env := msg.Envelope

		switch env.Type {
		case messages.TypePing:
			pong, err := messages.New(messages.TypePong, nil)
			if err != nil {
				logger.Warn("build pong failed", "event", "gateway.agent_pong_build_failed", "tunnel_id", s.tunnel.TunnelID, "subdomain", s.tunnel.Subdomain, "error", err)
				continue
			}
			if err := s.write(ctx, pong); err != nil {
				logger.Warn("write pong failed", "event", "gateway.agent_pong_write_failed", "tunnel_id", s.tunnel.TunnelID, "subdomain", s.tunnel.Subdomain, "error", err)
				return
			}
		case messages.TypeHTTPResponse,
			messages.TypeHTTPResponseStart,
			messages.TypeHTTPResponseBody,
			messages.TypeHTTPResponseEnd,
			messages.TypeHTTPStreamError,
			messages.TypeWSAccept,
			messages.TypeWSError,
			messages.TypeWSMessageText,
			messages.TypeWSMessageBinary,
			messages.TypeWSClose,
			messages.TypeWSCancel:
			if !s.deliver(ctx, msg) {
				logger.Warn("received response for unknown stream", "event", "gateway.unknown_stream_response", "tunnel_id", s.tunnel.TunnelID, "subdomain", s.tunnel.Subdomain, "stream_id", env.StreamID)
			}
		default:
			logger.Warn("unexpected agent message", "event", "gateway.unexpected_agent_message", "tunnel_id", s.tunnel.TunnelID, "subdomain", s.tunnel.Subdomain, "type", env.Type)
		}
	}
}

func (s *agentSession) addPending(streamID string, ch chan wswire.Message) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pending[streamID] = ch
}

func (s *agentSession) removePending(streamID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pending, streamID)
}

func (s *agentSession) deliver(ctx context.Context, msg wswire.Message) bool {
	s.pendingMu.RLock()
	ch, ok := s.pending[msg.Envelope.StreamID]
	s.pendingMu.RUnlock()
	if !ok {
		return false
	}

	select {
	case ch <- msg:
	case <-ctx.Done():
	case <-s.done:
	}
	return true
}

func (s *agentSession) closeDone() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (s *agentSession) close(status websocket.StatusCode, reason string) error {
	s.closeDone()
	return s.conn.Close(status, reason)
}

func (s *agentSession) closeNow() error {
	s.closeDone()
	return s.conn.CloseNow()
}

func contextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
