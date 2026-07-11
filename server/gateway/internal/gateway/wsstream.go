// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wsproxy"
	"github.com/voiteco/porthook/protocol/wswire"
)

// wsStreamPendingBuffer bounds how many undelivered messages a tunneled
// WebSocket stream buffers before the shared agent connection's read loop
// blocks delivering to it, providing backpressure toward the local service
// without growing memory without bound.
const wsStreamPendingBuffer = 64

// wsStream is an accepted WebSocket tunnel stream: the agent's local dial
// succeeded, and the caller now owns relaying frames in both directions
// until Close.
type wsStream struct {
	session  *agentSession
	streamID string
	pending  chan wswire.Message

	closeOnce sync.Once
}

// openWSStream asks the connected agent to dial the local target's
// WebSocket endpoint described by open, and blocks until the agent accepts,
// reports an error, the agent session ends, or ctx is done. On success the
// caller owns the returned stream and must call Close when finished
// relaying.
func (s *agentSession) openWSStream(ctx context.Context, streamID string, open wsproxy.Open) (wsproxy.Accept, *wsStream, error) {
	if err := s.acquireStream(); err != nil {
		return wsproxy.Accept{}, nil, err
	}

	ch := make(chan wswire.Message, wsStreamPendingBuffer)
	s.addPending(streamID, ch)
	cleanup := func() {
		s.removePending(streamID)
		s.releaseStream()
	}

	env, err := messages.NewStream(messages.TypeWSOpen, streamID, s.tunnel.TunnelID, open)
	if err != nil {
		cleanup()
		return wsproxy.Accept{}, nil, err
	}
	if err := s.write(ctx, env); err != nil {
		cleanup()
		return wsproxy.Accept{}, nil, fmt.Errorf("write ws open: %w", err)
	}

	select {
	case <-ctx.Done():
		cleanup()
		return wsproxy.Accept{}, nil, context.Cause(ctx)
	case <-s.done:
		cleanup()
		return wsproxy.Accept{}, nil, errors.New("agent disconnected")
	case msg := <-ch:
		switch msg.Envelope.Type {
		case messages.TypeWSAccept:
			accept, err := messages.DecodePayload[wsproxy.Accept](msg.Envelope)
			if err != nil {
				cleanup()
				return wsproxy.Accept{}, nil, err
			}
			return accept, &wsStream{session: s, streamID: streamID, pending: ch}, nil
		case messages.TypeWSError:
			payload, err := messages.DecodePayload[messages.ErrorPayload](msg.Envelope)
			cleanup()
			if err != nil {
				return wsproxy.Accept{}, nil, err
			}
			return wsproxy.Accept{}, nil, errors.New(payload.Message)
		default:
			cleanup()
			return wsproxy.Accept{}, nil, fmt.Errorf("unexpected ws open response %s", msg.Envelope.Type)
		}
	}
}

// Pending delivers messages the agent sends for this stream: WebSocket
// text/binary body frames, ws.close, or ws.cancel.
func (w *wsStream) Pending() <-chan wswire.Message {
	return w.pending
}

// SendMessage relays one WebSocket application message to the agent's local
// connection. typ must be messages.TypeWSMessageText or
// messages.TypeWSMessageBinary.
func (w *wsStream) SendMessage(ctx context.Context, typ messages.Type, data []byte) error {
	return w.session.writeBinaryBody(ctx, typ, w.streamID, data)
}

// SendClose tells the agent the public side closed gracefully with code and
// reason.
func (w *wsStream) SendClose(ctx context.Context, code int, reason string) error {
	env, err := messages.NewStream(messages.TypeWSClose, w.streamID, w.session.tunnel.TunnelID, wsproxy.Close{
		Code:   code,
		Reason: reason,
	})
	if err != nil {
		return err
	}
	return w.session.write(ctx, env)
}

// SendCancel tells the agent the stream is aborting abnormally, for reason.
func (w *wsStream) SendCancel(ctx context.Context, reason string) error {
	env, err := messages.NewStream(messages.TypeWSCancel, w.streamID, w.session.tunnel.TunnelID, messages.StreamCancel{
		Reason: reason,
	})
	if err != nil {
		return err
	}
	return w.session.write(ctx, env)
}

// Close releases the stream's pending registration and concurrent-stream
// slot. Safe to call more than once.
func (w *wsStream) Close() {
	w.closeOnce.Do(func() {
		w.session.removePending(w.streamID)
		w.session.releaseStream()
	})
}
