// SPDX-License-Identifier: Apache-2.0

package messages

import "testing"

func FuzzDecodeBinaryBodyFrame(f *testing.F) {
	valid, err := NewBinaryBodyFrame(TypeHTTPRequestBody, "str_test", "tun_test", []byte("payload"))
	if err != nil {
		f.Fatalf("build binary body frame seed: %v", err)
	}
	f.Add(valid)

	validWS, err := NewBinaryBodyFrame(TypeWSMessageBinary, "str_ws", "tun_ws", []byte("payload"))
	if err != nil {
		f.Fatalf("build WS binary body frame seed: %v", err)
	}
	f.Add(validWS)

	f.Add([]byte("PHB1"))
	f.Add([]byte("not-a-porthook-frame"))

	f.Fuzz(func(t *testing.T, frame []byte) {
		_, _ = DecodeBinaryBodyFrame(frame)
	})
}
