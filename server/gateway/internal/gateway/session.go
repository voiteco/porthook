// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"log/slog"
	"sync"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
)

type agentSession struct {
	conn   *websocket.Conn
	tunnel *registry.Session
	sendMu sync.Mutex
}

func newAgentSession(conn *websocket.Conn, tunnel *registry.Session) *agentSession {
	return &agentSession{
		conn:   conn,
		tunnel: tunnel,
	}
}

func (s *agentSession) write(ctx context.Context, env messages.Envelope) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return wsjson.Write(ctx, s.conn, env)
}

func (s *agentSession) readLoop(ctx context.Context, logger *slog.Logger) {
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
		default:
			logger.Warn("unexpected agent message", "tunnel_id", s.tunnel.TunnelID, "type", env.Type)
		}
	}
}
