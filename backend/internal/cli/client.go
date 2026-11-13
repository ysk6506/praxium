package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// requestTimeout bounds one request; a command line has no use for a hung call.
const requestTimeout = 30 * time.Second

// Client calls one server.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// NewClient builds a client for the settings. A nil httpClient means a
// default one with a timeout.
func NewClient(s Settings, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{base: s.Endpoint, token: s.Token, http: httpClient}
}

// Envelope is a success answer: data and meta stay raw so the caller reads what it needs.
type Envelope struct {
	Data json.RawMessage `json:"data"`
	Meta map[string]any  `json:"meta"`
}

// Get calls a GET route and returns the envelope, or the server's problem as a *ProblemError.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (*Envelope, error) {
	status, body, err := c.raw(ctx, path, query)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, problemOf(status, body)
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, transportError{err: err}
	}
	return &env, nil
}

// Status calls a GET route and returns only the status, for the health probes.
func (c *Client) Status(ctx context.Context, path string) (int, error) {
	status, _, err := c.raw(ctx, path, nil)
	return status, err
}

func (c *Client) raw(ctx context.Context, path string, query url.Values) (int, []byte, error) {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, usageError{msg: "the endpoint is not a URL: " + err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, transportError{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, nil, transportError{err: err}
	}
	return resp.StatusCode, body, nil
}

// problemOf reads an error body; a body that is not a problem still gives an error with the status.
func problemOf(status int, body []byte) error {
	var p ProblemError
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&p); err != nil || p.Code == "" {
		return &ProblemError{Status: status, Code: "server.internal", Title: strings.TrimSpace(string(body))}
	}
	return &p
}
