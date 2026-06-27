// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/voiteco/porthook/protocol/httpwire"
)

func ForwardHTTPRequest(ctx context.Context, client *http.Client, localTarget string, req httpwire.Request, maxResponseBodyBytes int64) (httpwire.Response, error) {
	targetURL, err := buildLocalURL(localTarget, req.Path, req.Query)
	if err != nil {
		return httpwire.Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL, bytes.NewReader(req.Body))
	if err != nil {
		return httpwire.Response{}, fmt.Errorf("build local request: %w", err)
	}
	httpReq.Header = httpwire.StripHopByHopHeaders(req.Header)

	resp, err := client.Do(httpReq)
	if err != nil {
		return httpwire.Response{}, fmt.Errorf("call local service: %w", err)
	}
	defer resp.Body.Close()

	body, err := readLimitedResponseBody(resp.Body, maxResponseBodyBytes)
	if err != nil {
		return httpwire.Response{}, fmt.Errorf("read local response: %w", err)
	}

	return httpwire.Response{
		Status: resp.StatusCode,
		Header: httpwire.StripHopByHopHeaders(resp.Header),
		Body:   body,
	}, nil
}

func ForwardHTTPStream(
	ctx context.Context,
	client *http.Client,
	localTarget string,
	req httpwire.RequestStart,
	body io.Reader,
	maxResponseBodyBytes int64,
	chunkBytes int,
	writeResponseStart func(httpwire.ResponseStart) error,
	writeResponseBody func([]byte) error,
) (int, int64, error) {
	targetURL, err := buildLocalURL(localTarget, req.Path, req.Query)
	if err != nil {
		return 0, 0, err
	}
	if body == nil {
		body = http.NoBody
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL, body)
	if err != nil {
		return 0, 0, fmt.Errorf("build local request: %w", err)
	}
	httpReq.Header = httpwire.StripHopByHopHeaders(req.Header)
	if req.ContentLength > 0 {
		httpReq.ContentLength = req.ContentLength
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, 0, fmt.Errorf("call local service: %w", err)
	}
	defer resp.Body.Close()

	start := httpwire.ResponseStart{
		Status: resp.StatusCode,
		Header: httpwire.StripHopByHopHeaders(resp.Header),
	}
	if err := writeResponseStart(start); err != nil {
		return resp.StatusCode, 0, err
	}

	responseBytes, err := streamLimitedResponseBody(resp.Body, maxResponseBodyBytes, chunkBytes, writeResponseBody)
	if err != nil {
		return resp.StatusCode, responseBytes, fmt.Errorf("read local response: %w", err)
	}
	return resp.StatusCode, responseBytes, nil
}

func streamLimitedResponseBody(body io.Reader, limit int64, chunkBytes int, writeChunk func([]byte) error) (int64, error) {
	if limit <= 0 {
		limit = defaultMaxResponseBodyBytes
	}
	if chunkBytes <= 0 {
		chunkBytes = defaultStreamChunkBytes
	}

	buf := make([]byte, chunkBytes)
	var total int64
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return total, fmt.Errorf("local response body exceeds %d bytes", limit)
			}
			chunk := append([]byte(nil), buf[:n]...)
			if err := writeChunk(chunk); err != nil {
				return total, err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func readLimitedResponseBody(body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = defaultMaxResponseBodyBytes
	}

	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("local response body exceeds %d bytes", limit)
	}
	return data, nil
}

func buildLocalURL(localTarget, requestPath, rawQuery string) (string, error) {
	base, err := url.Parse(localTarget)
	if err != nil {
		return "", fmt.Errorf("parse local target: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("local target must include scheme and host")
	}

	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}

	base.Path = joinURLPath(base.Path, requestPath)
	base.RawQuery = rawQuery
	base.Fragment = ""
	return base.String(), nil
}

func joinURLPath(basePath, requestPath string) string {
	switch {
	case basePath == "" || basePath == "/":
		return requestPath
	case strings.HasSuffix(basePath, "/") && strings.HasPrefix(requestPath, "/"):
		return basePath + strings.TrimPrefix(requestPath, "/")
	case !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(requestPath, "/"):
		return basePath + "/" + requestPath
	default:
		return basePath + requestPath
	}
}
