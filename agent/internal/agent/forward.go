// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
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
