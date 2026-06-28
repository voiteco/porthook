// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	binaryBodyMagic    = "PHB1"
	binaryBodyHeader   = len(binaryBodyMagic) + 1 + 2 + 2
	binaryRequestBody  = byte(1)
	binaryResponseBody = byte(2)
)

type BinaryBodyFrame struct {
	Type     Type
	StreamID string
	TunnelID string
	Data     []byte
}

func NewBinaryBodyFrame(typ Type, streamID, tunnelID string, data []byte) ([]byte, error) {
	frameType, err := binaryBodyFrameType(typ)
	if err != nil {
		return nil, err
	}
	if streamID == "" {
		return nil, errors.New("stream id is required")
	}
	if len(streamID) > 65535 {
		return nil, errors.New("stream id is too long")
	}
	if len(tunnelID) > 65535 {
		return nil, errors.New("tunnel id is too long")
	}

	frame := make([]byte, binaryBodyHeader+len(streamID)+len(tunnelID)+len(data))
	copy(frame[:len(binaryBodyMagic)], binaryBodyMagic)
	frame[len(binaryBodyMagic)] = frameType
	binary.BigEndian.PutUint16(frame[len(binaryBodyMagic)+1:], uint16(len(streamID)))
	binary.BigEndian.PutUint16(frame[len(binaryBodyMagic)+3:], uint16(len(tunnelID)))

	offset := binaryBodyHeader
	copy(frame[offset:], streamID)
	offset += len(streamID)
	copy(frame[offset:], tunnelID)
	offset += len(tunnelID)
	copy(frame[offset:], data)
	return frame, nil
}

func DecodeBinaryBodyFrame(frame []byte) (BinaryBodyFrame, error) {
	if len(frame) < binaryBodyHeader {
		return BinaryBodyFrame{}, errors.New("binary body frame is too short")
	}
	if string(frame[:len(binaryBodyMagic)]) != binaryBodyMagic {
		return BinaryBodyFrame{}, errors.New("invalid binary body frame magic")
	}

	typ, err := binaryBodyMessageType(frame[len(binaryBodyMagic)])
	if err != nil {
		return BinaryBodyFrame{}, err
	}
	streamLen := int(binary.BigEndian.Uint16(frame[len(binaryBodyMagic)+1:]))
	tunnelLen := int(binary.BigEndian.Uint16(frame[len(binaryBodyMagic)+3:]))
	if len(frame) < binaryBodyHeader+streamLen+tunnelLen {
		return BinaryBodyFrame{}, errors.New("binary body frame has truncated stream metadata")
	}

	offset := binaryBodyHeader
	streamID := string(frame[offset : offset+streamLen])
	offset += streamLen
	tunnelID := string(frame[offset : offset+tunnelLen])
	offset += tunnelLen
	if streamID == "" {
		return BinaryBodyFrame{}, errors.New("stream id is required")
	}

	return BinaryBodyFrame{
		Type:     typ,
		StreamID: streamID,
		TunnelID: tunnelID,
		Data:     frame[offset:],
	}, nil
}

func binaryBodyFrameType(typ Type) (byte, error) {
	switch typ {
	case TypeHTTPRequestBody:
		return binaryRequestBody, nil
	case TypeHTTPResponseBody:
		return binaryResponseBody, nil
	default:
		return 0, fmt.Errorf("unsupported binary body message type %s", typ)
	}
}

func binaryBodyMessageType(typ byte) (Type, error) {
	switch typ {
	case binaryRequestBody:
		return TypeHTTPRequestBody, nil
	case binaryResponseBody:
		return TypeHTTPResponseBody, nil
	default:
		return "", fmt.Errorf("unsupported binary body frame type %d", typ)
	}
}
