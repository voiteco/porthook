// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wsproxy"
)

// webSocketHandshakeHeaders are stripped from a forwarded WebSocket upgrade
// request in addition to the standard hop-by-hop set: the local dial and
// the public accept each perform their own handshake and must not see a
// stale key, version, extension, or protocol offer from the other leg.
var webSocketHandshakeHeaders = []string{
	"Sec-WebSocket-Key",
	"Sec-WebSocket-Version",
	"Sec-WebSocket-Extensions",
	"Sec-WebSocket-Accept",
	"Sec-WebSocket-Protocol",
}

func isWebSocketUpgradeRequest(r *http.Request) bool {
	return headerContainsToken(r.Header.Get("Upgrade"), "websocket") &&
		headerContainsToken(r.Header.Get("Connection"), "upgrade")
}

func headerContainsToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func requestedWebSocketSubprotocols(r *http.Request) []string {
	raw := r.Header.Get("Sec-WebSocket-Protocol")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var protocols []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			protocols = append(protocols, part)
		}
	}
	return protocols
}

func buildWSOpen(r *http.Request, stripAuthorization bool) wsproxy.Open {
	header := httpwire.StripHopByHopHeaders(r.Header)
	for _, name := range webSocketHandshakeHeaders {
		header.Del(name)
	}
	if stripAuthorization {
		header.Del("Authorization")
	}
	return wsproxy.Open{
		Path:         r.URL.Path,
		Query:        r.URL.RawQuery,
		Header:       header,
		Subprotocols: requestedWebSocketSubprotocols(r),
	}
}

// handlePublicWebSocket relays a public WebSocket upgrade through session to
// the agent's local target, applying deadline's request/idle/max-lifetime
// policy for the connection's whole lifetime. It returns the values
// handlePublicRequest's deferred logging/metrics closure expects.
func (s *Server) handlePublicWebSocket(
	w http.ResponseWriter,
	r *http.Request,
	session *agentSession,
	accessResult accessPolicyEvaluation,
	streamID string,
	deadline *streamDeadline,
) (status int, outcome string, requestBytes, responseBytes int64, err error) {
	if !session.tunnel.HasCapability(messages.CapabilityWebSocketTunnel) {
		writePublicError(w, r, "the connected agent does not support WebSocket tunneling", http.StatusNotImplemented)
		return http.StatusNotImplemented, "websocket_unsupported", 0, 0, nil
	}

	open := buildWSOpen(r, accessConsumesAuthorization(accessResult))

	ctx := deadline.Context()
	accept, stream, err := session.openWSStream(ctx, streamID, open)
	if err != nil {
		switch {
		case errors.Is(err, ErrStreamLimitExceeded):
			writePublicError(w, r, "tunnel overloaded", http.StatusServiceUnavailable)
			return http.StatusServiceUnavailable, "stream_limit_exceeded", 0, 0, err
		case errors.Is(err, ErrStreamRequestTimeout), errors.Is(err, context.DeadlineExceeded):
			writePublicError(w, r, "tunnel request timed out", http.StatusGatewayTimeout)
			return http.StatusGatewayTimeout, "tunnel_timeout", 0, 0, err
		case errors.Is(err, context.Canceled):
			return 499, "client_canceled", 0, 0, err
		default:
			writePublicError(w, r, "websocket tunnel failed", http.StatusBadGateway)
			return http.StatusBadGateway, "tunnel_error", 0, 0, err
		}
	}
	defer stream.Close()

	acceptOpts := &websocket.AcceptOptions{}
	if accept.Subprotocol != "" {
		acceptOpts.Subprotocols = []string{accept.Subprotocol}
	}
	publicConn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		_ = stream.SendCancel(context.Background(), "public websocket handshake failed")
		return 0, "websocket_handshake_failed", 0, 0, err
	}
	defer publicConn.CloseNow()

	deadline.MarkResponseStarted()

	requestBytes, responseBytes, relayErr := relayPublicWebSocket(ctx, s.cfg.WebSocketWriteTimeout, s.cfg.WSMessageMaxBytes, publicConn, stream, deadline)
	if relayErr != nil {
		switch {
		case errors.Is(relayErr, ErrStreamIdleTimeout):
			return http.StatusSwitchingProtocols, "stream_idle_timeout", requestBytes, responseBytes, relayErr
		case errors.Is(relayErr, ErrStreamMaxLifetimeExceeded):
			return http.StatusSwitchingProtocols, "stream_max_lifetime_exceeded", requestBytes, responseBytes, relayErr
		case errors.Is(relayErr, context.Canceled):
			return http.StatusSwitchingProtocols, "client_canceled", requestBytes, responseBytes, relayErr
		default:
			return http.StatusSwitchingProtocols, "websocket_error", requestBytes, responseBytes, relayErr
		}
	}
	return http.StatusSwitchingProtocols, "completed", requestBytes, responseBytes, nil
}

// relayPublicWebSocket pumps frames between the public client connection and
// the tunneled agent stream until either side closes or ctx ends, and
// returns the bytes relayed in each direction. deadline.Touch is called on
// every relayed message so an active-but-slow connection is never cut by
// the idle timeout, while ctx (deadline.Context()) still bounds the whole
// relay's maximum lifetime.
func relayPublicWebSocket(
	ctx context.Context,
	writeTimeout time.Duration,
	maxMessageBytes int64,
	publicConn *websocket.Conn,
	stream *wsStream,
	deadline *streamDeadline,
) (int64, int64, error) {
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()

	if maxMessageBytes > 0 {
		publicConn.SetReadLimit(maxMessageBytes)
	}

	var requestBytes, responseBytes atomic.Int64
	errCh := make(chan error, 2)

	go func() {
		errCh <- pumpPublicToAgent(relayCtx, writeTimeout, publicConn, stream, deadline, &requestBytes)
	}()
	go func() {
		errCh <- pumpAgentToPublic(relayCtx, writeTimeout, publicConn, stream, deadline, &responseBytes)
	}()

	firstErr := <-errCh
	cancelRelay()
	<-errCh

	// A deadline (request/idle/max-lifetime) or parent cancellation ends
	// the relay without either pump having told the agent why; every other
	// exit path already notified it via ws.close or ws.cancel.
	if isStreamContextDoneCause(firstErr) {
		_ = stream.SendCancel(context.Background(), streamCancelReason(firstErr))
	}

	return requestBytes.Load(), responseBytes.Load(), firstErr
}

func pumpPublicToAgent(
	ctx context.Context,
	writeTimeout time.Duration,
	publicConn *websocket.Conn,
	stream *wsStream,
	deadline *streamDeadline,
	byteCount *atomic.Int64,
) error {
	for {
		msgType, data, err := publicConn.Read(ctx)
		if err != nil {
			if cause := context.Cause(ctx); ctx.Err() != nil {
				return cause
			}
			return closeOrCancelForReadError(ctx, stream, err)
		}
		deadline.Touch()
		byteCount.Add(int64(len(data)))

		frameType := messages.TypeWSMessageBinary
		if msgType == websocket.MessageText {
			frameType = messages.TypeWSMessageText
		}
		writeCtx, cancel := contextWithTimeout(ctx, writeTimeout)
		sendErr := stream.SendMessage(writeCtx, frameType, data)
		cancel()
		if sendErr != nil {
			return sendErr
		}
	}
}

func pumpAgentToPublic(
	ctx context.Context,
	writeTimeout time.Duration,
	publicConn *websocket.Conn,
	stream *wsStream,
	deadline *streamDeadline,
	byteCount *atomic.Int64,
) error {
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case msg, ok := <-stream.Pending():
			if !ok {
				return errors.New("agent stream closed")
			}
			switch msg.Envelope.Type {
			case messages.TypeWSMessageText, messages.TypeWSMessageBinary:
				deadline.Touch()
				byteCount.Add(int64(len(msg.Body)))
				wsType := websocket.MessageBinary
				if msg.Envelope.Type == messages.TypeWSMessageText {
					wsType = websocket.MessageText
				}
				writeCtx, cancel := contextWithTimeout(ctx, writeTimeout)
				writeErr := publicConn.Write(writeCtx, wsType, msg.Body)
				cancel()
				if writeErr != nil {
					return writeErr
				}
			case messages.TypeWSClose:
				payload, err := messages.DecodePayload[wsproxy.Close](msg.Envelope)
				if err != nil {
					return err
				}
				_ = publicConn.Close(websocket.StatusCode(payload.Code), payload.Reason)
				return nil
			case messages.TypeWSCancel:
				payload, _ := messages.DecodePayload[messages.StreamCancel](msg.Envelope)
				return fmt.Errorf("agent canceled the websocket stream: %s", payload.Reason)
			default:
				return fmt.Errorf("unexpected websocket relay message %s", msg.Envelope.Type)
			}
		}
	}
}

// closeOrCancelForReadError tells the agent about a public-side WebSocket
// closure. A clean close (a real WebSocket close frame) is relayed as
// ws.close with the same code and reason; anything else (network failure,
// protocol violation) is relayed as ws.cancel.
func closeOrCancelForReadError(ctx context.Context, stream *wsStream, err error) error {
	if status := websocket.CloseStatus(err); status != -1 {
		_ = stream.SendClose(ctx, int(status), "")
		return nil
	}
	_ = stream.SendCancel(context.Background(), "public websocket connection failed")
	return err
}
