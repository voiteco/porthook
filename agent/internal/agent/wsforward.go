// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"nhooyr.io/websocket"

	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wsproxy"
	"github.com/voiteco/porthook/protocol/wswire"
)

// wsStreamPendingBuffer bounds how many undelivered gateway messages a
// tunneled WebSocket stream buffers before the shared gateway connection's
// read loop blocks delivering to it, providing backpressure toward the
// local WebSocket service without growing memory without bound.
const wsStreamPendingBuffer = 64

// handleWSStream dials the local target's WebSocket endpoint described by
// open and relays frames between it and the gateway for the lifetime of the
// tunneled connection.
func (r *Runner) handleWSStream(
	ctx context.Context,
	conn *websocket.Conn,
	tunnelID string,
	env messages.Envelope,
	open wsproxy.Open,
	inbound <-chan wswire.Message,
) {
	localURL, err := buildLocalWSURL(r.cfg.LocalTarget, open.Path, open.Query)
	if err != nil {
		r.sendWSError(ctx, conn, tunnelID, env.StreamID, "invalid_local_target", err.Error())
		return
	}

	dialCtx, cancel := contextWithTimeout(ctx, r.cfg.RequestTimeout)
	localConn, _, dialErr := websocket.Dial(dialCtx, localURL, &websocket.DialOptions{
		HTTPHeader:   open.Header,
		Subprotocols: open.Subprotocols,
	})
	cancel()
	if dialErr != nil {
		r.sendWSError(ctx, conn, tunnelID, env.StreamID, "local_dial_failed", fmt.Sprintf("dial local websocket endpoint: %v", dialErr))
		return
	}
	defer localConn.CloseNow()
	if r.cfg.WSMessageMaxBytes > 0 {
		localConn.SetReadLimit(r.cfg.WSMessageMaxBytes)
	}

	accept, err := messages.NewStream(messages.TypeWSAccept, env.StreamID, tunnelID, wsproxy.Accept{
		Subprotocol: localConn.Subprotocol(),
	})
	if err != nil {
		r.logger.Warn("build ws accept failed", "stream_id", env.StreamID, "error", err)
		return
	}
	if err := r.write(ctx, conn, accept); err != nil {
		r.logger.Warn("write ws accept failed", "stream_id", env.StreamID, "error", err)
		return
	}

	requestBytes, responseBytes, relayErr := r.relayWSStream(ctx, conn, tunnelID, env.StreamID, localConn, inbound)
	r.logWSStream(env.StreamID, tunnelID, open.Path, open.Query, requestBytes, responseBytes, relayErr)
}

func (r *Runner) sendWSError(ctx context.Context, conn *websocket.Conn, tunnelID, streamID, code, message string) {
	env, err := messages.NewStream(messages.TypeWSError, streamID, tunnelID, messages.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		r.logger.Warn("build ws error failed", "stream_id", streamID, "error", err)
		return
	}
	if err := r.write(ctx, conn, env); err != nil {
		r.logger.Warn("write ws error failed", "stream_id", streamID, "error", err)
	}
}

// relayWSStream pumps frames between the local WebSocket connection and the
// gateway tunnel until either side closes or ctx ends, and returns the
// bytes relayed in each direction (request = local-to-gateway, response =
// gateway-to-local, matching the direction names used elsewhere for this
// stream in logs).
func (r *Runner) relayWSStream(
	ctx context.Context,
	conn *websocket.Conn,
	tunnelID, streamID string,
	localConn *websocket.Conn,
	inbound <-chan wswire.Message,
) (int64, int64, error) {
	relayCtx, cancelRelay := context.WithCancel(ctx)
	defer cancelRelay()

	var localToGateway, gatewayToLocal atomic.Int64
	errCh := make(chan error, 2)

	go func() {
		errCh <- r.pumpLocalToGateway(relayCtx, conn, tunnelID, streamID, localConn, &localToGateway)
	}()
	go func() {
		errCh <- r.pumpGatewayToLocal(relayCtx, localConn, inbound, &gatewayToLocal)
	}()

	firstErr := <-errCh
	cancelRelay()
	<-errCh

	return localToGateway.Load(), gatewayToLocal.Load(), firstErr
}

func (r *Runner) pumpLocalToGateway(
	ctx context.Context,
	conn *websocket.Conn,
	tunnelID, streamID string,
	localConn *websocket.Conn,
	byteCount *atomic.Int64,
) error {
	for {
		msgType, data, err := localConn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return r.closeOrCancelForLocalReadError(ctx, conn, tunnelID, streamID, err)
		}
		byteCount.Add(int64(len(data)))

		frameType := messages.TypeWSMessageBinary
		if msgType == websocket.MessageText {
			frameType = messages.TypeWSMessageText
		}
		writeCtx, cancel := contextWithTimeout(ctx, r.cfg.WebSocketWriteTimeout)
		sendErr := r.writeBinaryBody(writeCtx, conn, frameType, streamID, tunnelID, data)
		cancel()
		if sendErr != nil {
			return sendErr
		}
	}
}

func (r *Runner) pumpGatewayToLocal(
	ctx context.Context,
	localConn *websocket.Conn,
	inbound <-chan wswire.Message,
	byteCount *atomic.Int64,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-inbound:
			if !ok {
				return errors.New("gateway stream closed")
			}
			switch msg.Envelope.Type {
			case messages.TypeWSMessageText, messages.TypeWSMessageBinary:
				byteCount.Add(int64(len(msg.Body)))
				wsType := websocket.MessageBinary
				if msg.Envelope.Type == messages.TypeWSMessageText {
					wsType = websocket.MessageText
				}
				writeCtx, cancel := contextWithTimeout(ctx, r.cfg.WebSocketWriteTimeout)
				writeErr := localConn.Write(writeCtx, wsType, msg.Body)
				cancel()
				if writeErr != nil {
					return writeErr
				}
			case messages.TypeWSClose:
				payload, err := messages.DecodePayload[wsproxy.Close](msg.Envelope)
				if err != nil {
					return err
				}
				_ = localConn.Close(websocket.StatusCode(payload.Code), payload.Reason)
				return nil
			case messages.TypeWSCancel:
				payload, _ := messages.DecodePayload[messages.StreamCancel](msg.Envelope)
				return fmt.Errorf("gateway canceled the websocket stream: %s", payload.Reason)
			default:
				return fmt.Errorf("unexpected websocket relay message %s", msg.Envelope.Type)
			}
		}
	}
}

// closeOrCancelForLocalReadError tells the gateway about a local-side
// WebSocket closure. A clean close (a real WebSocket close frame) is
// relayed as ws.close with the same code and reason; anything else
// (network failure, protocol violation) is relayed as ws.cancel.
func (r *Runner) closeOrCancelForLocalReadError(ctx context.Context, conn *websocket.Conn, tunnelID, streamID string, err error) error {
	if status := websocket.CloseStatus(err); status != -1 {
		closeEnv, buildErr := messages.NewStream(messages.TypeWSClose, streamID, tunnelID, wsproxy.Close{
			Code: int(status),
		})
		if buildErr == nil {
			_ = r.write(ctx, conn, closeEnv)
		}
		return nil
	}
	cancelEnv, buildErr := messages.NewStream(messages.TypeWSCancel, streamID, tunnelID, messages.StreamCancel{
		Reason: "local websocket connection failed",
	})
	if buildErr == nil {
		_ = r.write(context.Background(), conn, cancelEnv)
	}
	return err
}

func (r *Runner) logWSStream(streamID, tunnelID, path, query string, requestBytes, responseBytes int64, err error) {
	outcome := "completed"
	if err != nil {
		outcome = "local_websocket_failed"
	}
	r.logLocalRequestFields(streamID, tunnelID, "WS", path, query, 0, outcome, requestBytes, responseBytes, 0, err)
}
