// Package internalhttp is a small JSON client used for synchronous
// service-to-service calls authenticated with the internal API key.
package internalhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fayrly/backend/internal/platform/apierr"
)

type Client struct {
	baseURL string
	key     string
	http    *http.Client
}

func New(baseURL, key string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: timeout},
	}
}

type errorEnvelope struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	} `json:"error"`
}

// Do performs a JSON request against another service. It translates remote
// error envelopes into *apierr.Error so callers can propagate 4xx responses.
func (c *Client) Do(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode internal request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build internal request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Internal-Key", c.key)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return apierr.ServiceUnavailable("could not reach an internal service")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read internal response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var envelope errorEnvelope
		if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Message != "" {
			return &apierr.Error{
				Status:  resp.StatusCode,
				Code:    envelope.Error.Code,
				Message: envelope.Error.Message,
				Details: envelope.Error.Details,
			}
		}
		return apierr.ServiceUnavailable(fmt.Sprintf("internal service returned status %d", resp.StatusCode))
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode internal response: %w", err)
		}
	}
	return nil
}
