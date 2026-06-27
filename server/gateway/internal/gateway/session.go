// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
)

type agentSession struct {
	conn     *websocket.Conn
	tunnel   *registry.Session
	sendMu   sync.Mutex
	writeTO  time.Duration
	done     chan struct{}
	doneOnce sync.Once

	pendingMu sync.RWMutex
	pending   map[string]chan messages.Envelope
}

func newAgentSession(conn *websocket.Conn, tunnel *registry.Session, writeTimeout time.Duration) *agentSession {
	return &agentSession{
		conn:    conn,
		tunnel:  tunnel,
		writeTO: writeTimeout,
		done:    make(chan struct{}),
		pending: make(map[string]chan messages.Envelope),
	}
}

func (s *agentSession) roundTrip(ctx context.Context, streamID string, req httpwire.Request) (httpwire.Response, error) {
	ch := make(chan messages.Envelope, 1)
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
		return httpwire.Response{}, ctx.Err()
	case <-s.done:
		return httpwire.Response{}, errors.New("agent disconnected")
	case respEnv := <-ch:
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

func (s *agentSession) write(ctx context.Context, env messages.Envelope) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	writeCtx, cancel := contextWithTimeout(ctx, s.writeTO)
	defer cancel()
	return wsjson.Write(writeCtx, s.conn, env)
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
		var env messages.Envelope
		if err := wsjson.Read(ctx, s.conn, &env); err != nil {
			logger.Info("agent read loop stopped", "tunnel_id", s.tunnel.TunnelID, "error", err)
			return
		}

		switch env.Type {
		case messages.TypePing:
			pong, err := messages.New(messages.TypePong, nil)
			if err != nil {
				logger.Warn("build pong failed", "error", err)
				continue
			}
			if err := s.write(ctx, pong); err != nil {
				logger.Warn("write pong failed", "tunnel_id", s.tunnel.TunnelID, "error", err)
				return
			}
		case messages.TypeHTTPResponse, messages.TypeHTTPStreamError:
			if !s.deliver(env) {
				logger.Warn("received response for unknown stream", "tunnel_id", s.tunnel.TunnelID, "stream_id", env.StreamID)
			}
		default:
			logger.Warn("unexpected agent message", "tunnel_id", s.tunnel.TunnelID, "type", env.Type)
		}
	}
}

func (s *agentSession) addPending(streamID string, ch chan messages.Envelope) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pending[streamID] = ch
}

func (s *agentSession) removePending(streamID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pending, streamID)
}

func (s *agentSession) deliver(env messages.Envelope) bool {
	s.pendingMu.RLock()
	ch, ok := s.pending[env.StreamID]
	s.pendingMu.RUnlock()
	if !ok {
		return false
	}

	select {
	case ch <- env:
	default:
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
