package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/syncproto"
)

const (
	// defaultTimeout bounds a request when the caller supplies no http.Client.
	defaultTimeout = 30 * time.Second
	// maxResponseBytes caps how much of a response body is read.
	maxResponseBytes = 64 << 20
)

// Error codes of the server envelope outside the domain catalog.
const (
	codeUnauthorized     = "unauthorized"
	codeInvalidBody      = "invalid_body"
	codeInvalidParameter = "invalid_parameter"
)

var _ app.SyncRemote = (*Client)(nil)

// Client talks to one okt-server with one bearer token.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// errorEnvelope is the server's JSON error body.
type errorEnvelope struct {
	Code    string         `json:"code"`
	Message string         `json:"msg"`
	Details map[string]any `json:"details"`
}

// New returns a client for the server at baseURL. A nil hc gets a client with a default timeout.
// insecure allows plain http to a non-loopback host. The client never follows
// redirects, so the token reaches only baseURL.
func New(baseURL, token string, insecure bool, hc *http.Client) (*Client, error) {
	if err := domain.ValidateSyncRemoteURL(baseURL, insecure); err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	noRedirects := *hc
	noRedirects.CheckRedirect = refuseRedirect
	hc = &noRedirects
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, hc: hc}, nil
}

// refuseRedirect stops the client at the first redirect; do reports it as an error.
func refuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// ListProjects returns the projects the server holds.
func (c *Client) ListProjects(ctx context.Context) ([]domain.SyncProjectInfo, error) {
	var resp syncproto.ProjectsResponse
	if err := c.do(ctx, http.MethodGet, syncproto.ProjectsPath, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Projects, nil
}

// Push sends changes for a project and returns the server cursor and how many
// changes the server rejected.
func (c *Client) Push(ctx context.Context, info domain.SyncProjectInfo, changes []domain.SyncChange) (int64, int, error) {
	var resp syncproto.PushResponse
	req := syncproto.PushRequest{Project: info, Changes: changes}
	if err := c.do(ctx, http.MethodPost, syncproto.ChangesPath(info.UID), req, &resp); err != nil {
		return 0, 0, err
	}
	return resp.Cursor, resp.Rejected, nil
}

// Pull returns up to limit changes after the cursor and whether more follow.
func (c *Client) Pull(ctx context.Context, projectUID domain.UID, after int64, limit int) ([]domain.SyncChange, int64, bool, error) {
	var resp syncproto.PullResponse
	path := syncproto.ChangesPath(projectUID) + "?" + syncproto.PullQuery(after, limit)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, 0, false, err
	}
	return resp.Changes, resp.Cursor, resp.More, nil
}

// do sends one request and decodes a 2xx JSON body into out.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode sync request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("sync server %s: %w", c.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return c.redirectError(resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("sync server %s: read response: %w", c.baseURL, err)
	}
	if len(raw) > maxResponseBytes {
		return domain.NewError(domain.ErrPayloadTooLarge,
			fmt.Sprintf("sync server %s: response too large (over %d bytes)", c.baseURL, maxResponseBytes),
			map[string]any{"max_bytes": maxResponseBytes})
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.statusError(resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("sync server %s: decode response (status %d): %w", c.baseURL, resp.StatusCode, err)
	}
	return nil
}

// redirectError reports a redirect by its target host; the request is not repeated there.
func (c *Client) redirectError(resp *http.Response) error {
	target := resp.Header.Get("Location")
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		target = u.Host
	}
	return fmt.Errorf("sync server %s: refused redirect (status %d) to %q; check the remote URL", c.baseURL, resp.StatusCode, target)
}

// statusError maps a non-2xx response to a domain coded error (any 413 is payload-too-large, whatever its body), or to a status-bearing error when the body is no envelope.
func (c *Client) statusError(status int, raw []byte) error {
	if status == http.StatusRequestEntityTooLarge {
		return domain.NewError(domain.ErrPayloadTooLarge,
			fmt.Sprintf("sync server %s: request body too large (status 413)", c.baseURL), nil)
	}
	var env errorEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Code == "" {
		return fmt.Errorf("sync server %s: unexpected response, status %d", c.baseURL, status)
	}
	switch env.Code {
	case codeUnauthorized:
		return domain.NewError(domain.ErrOperationDenied, env.Message, env.Details)
	case string(domain.ErrProjectNotFound):
		return domain.NewError(domain.ErrProjectNotFound, env.Message, env.Details)
	case string(domain.ErrPayloadTooLarge):
		return domain.NewError(domain.ErrPayloadTooLarge, env.Message, env.Details)
	case string(domain.ErrValidation), codeInvalidBody, codeInvalidParameter:
		return domain.NewError(domain.ErrValidation, env.Message, env.Details)
	}
	return fmt.Errorf("sync server %s: status %d: %s: %s", c.baseURL, status, env.Code, env.Message)
}
