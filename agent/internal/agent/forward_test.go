// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voiteco/porthook/protocol/httpwire"
)

func TestForwardHTTPRequest(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/webhook" {
			t.Errorf("path = %s, want /webhook", r.URL.Path)
		}
		if r.URL.RawQuery != "source=test" {
			t.Errorf("query = %s, want source=test", r.URL.RawQuery)
		}
		if r.Header.Get("Connection") != "" {
			t.Errorf("Connection header was forwarded")
		}
		if r.Header.Get("X-Test") != "yes" {
			t.Errorf("X-Test = %q, want yes", r.Header.Get("X-Test"))
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll returned error: %v", err)
		}
		if string(body) != "payload" {
			t.Errorf("body = %q, want payload", string(body))
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	defer local.Close()

	req := httpwire.Request{
		Method: http.MethodPost,
		Path:   "/webhook",
		Query:  "source=test",
		Header: http.Header{
			"X-Test":     []string{"yes"},
			"Connection": []string{"close"},
		},
		Body: []byte("payload"),
	}

	resp, err := ForwardHTTPRequest(context.Background(), local.Client(), local.URL, req, defaultMaxResponseBodyBytes)
	if err != nil {
		t.Fatalf("ForwardHTTPRequest returned error: %v", err)
	}
	if resp.Status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.Status)
	}
	if resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Connection") != "" {
		t.Fatal("Connection response header was not stripped")
	}
	if string(resp.Body) != "ok" {
		t.Fatalf("body = %q, want ok", string(resp.Body))
	}
}

func TestForwardHTTPRequestRejectsLargeResponse(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("too-large"))
	}))
	defer local.Close()

	req := httpwire.Request{
		Method: http.MethodGet,
		Path:   "/",
	}

	_, err := ForwardHTTPRequest(context.Background(), local.Client(), local.URL, req, 3)
	if err == nil {
		t.Fatal("ForwardHTTPRequest returned nil error")
	}
}

func TestForwardHTTPStream(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.ContentLength != 7 {
			t.Errorf("content length = %d, want 7", r.ContentLength)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll returned error: %v", err)
		}
		if string(body) != "payload" {
			t.Errorf("body = %q, want payload", string(body))
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("streamed-response"))
	}))
	defer local.Close()

	var responseStart httpwire.ResponseStart
	var responseBody bytes.Buffer
	status, responseBytes, err := ForwardHTTPStream(
		context.Background(),
		local.Client(),
		local.URL,
		httpwire.RequestStart{
			Method:        http.MethodPost,
			Path:          "/webhook",
			Header:        http.Header{"Content-Type": []string{"text/plain"}},
			ContentLength: 7,
		},
		bytes.NewBufferString("payload"),
		defaultMaxResponseBodyBytes,
		4,
		func(start httpwire.ResponseStart) error {
			responseStart = start
			return nil
		},
		func(chunk []byte) error {
			_, err := responseBody.Write(chunk)
			return err
		},
	)
	if err != nil {
		t.Fatalf("ForwardHTTPStream returned error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if responseStart.Status != http.StatusAccepted {
		t.Fatalf("response start status = %d, want 202", responseStart.Status)
	}
	if responseStart.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", responseStart.Header.Get("Content-Type"))
	}
	if responseBytes != int64(len("streamed-response")) {
		t.Fatalf("response bytes = %d, want %d", responseBytes, len("streamed-response"))
	}
	if responseBody.String() != "streamed-response" {
		t.Fatalf("response body = %q, want streamed-response", responseBody.String())
	}
}

func TestBuildLocalURL(t *testing.T) {
	got, err := buildLocalURL("http://localhost:3000/base", "/webhook", "a=b")
	if err != nil {
		t.Fatalf("buildLocalURL returned error: %v", err)
	}
	want := "http://localhost:3000/base/webhook?a=b"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}
