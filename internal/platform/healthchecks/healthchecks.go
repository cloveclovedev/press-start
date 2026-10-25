// Package healthchecks reports to Healthchecks.io that the service runs, or
// that it fails.
//
// The client uses the Pinging API (https://healthchecks.io/docs/http_api/):
// a GET to the ping URL is a success, and a POST to the ping URL with the
// suffix "/fail" is a failure. The ping URL holds the key of the check, so
// no error of this package holds the URL.
package healthchecks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Timeout is the longest time that one request waits. A monitor that does
// not answer must not hold up the service.
const Timeout = 10 * time.Second

// Client sends pings to one check.
type Client struct {
	pingURL string
	timeout time.Duration
	http    *http.Client
}

// New returns a Client for the check of pingURL, such as
// "https://hc-ping.com/<uuid>".
func New(pingURL string) *Client {
	return &Client{
		pingURL: strings.TrimSuffix(pingURL, "/"),
		timeout: Timeout,
		http:    http.DefaultClient,
	}
}

// Success tells Healthchecks.io that the service runs.
func (c *Client) Success(ctx context.Context) error {
	return c.send(ctx, "success", http.MethodGet, c.pingURL, "")
}

// Fail tells Healthchecks.io that the service fails. Body is the text that
// the check shows with the failure.
func (c *Client) Fail(ctx context.Context, body string) error {
	return c.send(ctx, "fail", http.MethodPost, c.pingURL+"/fail", body)
}

func (c *Client) send(ctx context.Context, name, method, target, body string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("healthchecks: %s: %w", name, withoutURL(err))
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("healthchecks: %s: %w", name, withoutURL(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("healthchecks: %s: status %d", name, resp.StatusCode)
	}
	return nil
}

// withoutURL returns the cause of a *url.Error, because the text of a
// *url.Error holds the URL.
func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
