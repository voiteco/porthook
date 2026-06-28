// SPDX-License-Identifier: Apache-2.0

package wswire

import (
	"context"
	"encoding/json"
	"fmt"

	"nhooyr.io/websocket"

	"github.com/voiteco/porthook/protocol/messages"
)

type Message struct {
	Envelope   messages.Envelope
	Body       []byte
	BinaryBody bool
}

func Read(ctx context.Context, conn *websocket.Conn) (Message, error) {
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		return Message{}, err
	}

	switch messageType {
	case websocket.MessageText:
		var env messages.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return Message{}, fmt.Errorf("decode websocket json message: %w", err)
		}
		return Message{Envelope: env}, nil
	case websocket.MessageBinary:
		frame, err := messages.DecodeBinaryBodyFrame(data)
		if err != nil {
			return Message{}, fmt.Errorf("decode websocket binary body frame: %w", err)
		}
		env, err := messages.NewStream(frame.Type, frame.StreamID, frame.TunnelID, nil)
		if err != nil {
			return Message{}, err
		}
		return Message{
			Envelope:   env,
			Body:       frame.Data,
			BinaryBody: true,
		}, nil
	default:
		return Message{}, fmt.Errorf("unsupported websocket message type %s", messageType)
	}
}

func WriteEnvelope(ctx context.Context, conn *websocket.Conn, env messages.Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode websocket json message: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func WriteBinaryBody(ctx context.Context, conn *websocket.Conn, typ messages.Type, streamID, tunnelID string, data []byte) error {
	frame, err := messages.NewBinaryBodyFrame(typ, streamID, tunnelID, data)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, frame)
}
