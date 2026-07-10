// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"encoding/json"
	"testing"
)

func FuzzDecodeEnvelope(f *testing.F) {
	f.Add([]byte(`{"type":"auth.request","payload":{"token":"test","protocol_version":"0.2"}}`))
	f.Add([]byte(`{"type":"http.request.start","stream_id":"str_test","payload":{"method":"GET","path":"/"}}`))
	f.Add([]byte(`not-json`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var envelope Envelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return
		}

		switch envelope.Type {
		case TypeAuthRequest:
			_, _ = DecodePayload[AuthRequest](envelope)
		case TypeTunnelRegister:
			_, _ = DecodePayload[TunnelRegister](envelope)
		case TypeHTTPStreamCancel:
			_, _ = DecodePayload[StreamCancel](envelope)
		default:
			_, _ = DecodePayload[map[string]any](envelope)
		}
	})
}
