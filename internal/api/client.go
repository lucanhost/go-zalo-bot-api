// Package api implements the low-level Zalo Bot HTTP transport used by the
// zalobot package. It is internal and not part of the public API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 4 << 20

// Client is a minimal JSON transport for the Zalo Bot API.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient returns a Client for the given token and base URL. A nil http
// client falls back to http.DefaultClient.
func NewClient(token, baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

// URL returns the request URL for a method, including the bot token.
func (c *Client) URL(method string) string {
	return c.baseURL + "/bot" + c.token + "/" + method
}

// RedactedURL returns the request URL with the bot token replaced by
// "<TOKEN>", for safe use in error messages.
func (c *Client) RedactedURL(method string) string {
	return c.baseURL + "/bot<TOKEN>/" + method
}

// Call posts params as JSON to method and returns the raw `result` field. It
// returns an *Error, *TransportError, or *DecodeError on failure.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var body io.Reader
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, &DecodeError{Method: method, Err: err}
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(method), body)
	if err != nil {
		return nil, &TransportError{Method: method, RedactedURL: c.RedactedURL(method), Err: unwrapURLErr(err)}
	}
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &TransportError{Method: method, RedactedURL: c.RedactedURL(method), Err: unwrapURLErr(err)}
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, &TransportError{Method: method, RedactedURL: c.RedactedURL(method), Err: err}
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, &DecodeError{Method: method, HTTPStatus: resp.StatusCode, Err: err}
	}
	if !env.OK {
		code := env.ErrorCode
		if code == 0 {
			code = env.ErrorCodeCamel
		}
		if code == 0 {
			code = resp.StatusCode
		}
		return nil, &Error{Code: code, Description: env.Description, Method: method, HTTPStatus: resp.StatusCode}
	}
	return env.Result, nil
}

func unwrapURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
