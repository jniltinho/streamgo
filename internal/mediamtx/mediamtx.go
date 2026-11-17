// Package mediamtx reaches MediaMTX for the live output (design D3): the WHIP client behind
// StreamGo's WHIP routes and the control API client that finds and closes the session
// publishing a broadcast's program (calls confirmed on MediaMTX 1.21.4 in the spike). It
// implements output.Ingest. Every call carries the internal credentials, follows no redirect
// and gives up after Timeout.
package mediamtx

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

	"github.com/jniltinho/streamgo/internal/core/output"
)

// Timeout bounds every call to MediaMTX.
const Timeout = 10 * time.Second

// maxBody bounds what is read of an answer.
const maxBody = 1 << 20

// webRTCSession is the source type of a path published by WHIP.
const webRTCSession = "webRTCSession"

// Client is a MediaMTX instance.
type Client struct {
	whip, api    *url.URL
	user, secret string
	http         *http.Client
}

var _ output.Ingest = (*Client)(nil)

// New returns a client of the MediaMTX whose WebRTC server is whipURL and control API apiURL.
func New(whipURL, apiURL, user, secret string) (*Client, error) {
	w, err := url.Parse(strings.TrimSuffix(whipURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("mediamtx.whip_url: %w", err)
	}
	a, err := url.Parse(strings.TrimSuffix(apiURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("mediamtx.api_url: %w", err)
	}
	return &Client{whip: w, api: a, user: user, secret: secret, http: &http.Client{
		Timeout:       Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Publish posts the WHIP offer to the broadcast's program path.
func (c *Client) Publish(ctx context.Context, broadcastID string, offer []byte) (output.Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	endpoint := c.whip.JoinPath("program", broadcastID, "whip")
	res, body, err := c.do(ctx, http.MethodPost, endpoint.String(), "application/sdp", offer)
	if err != nil {
		return output.Answer{}, fmt.Errorf("%w: %w", output.ErrUpstream, err)
	}
	if res.StatusCode != http.StatusCreated {
		return output.Answer{}, fmt.Errorf("%w: WHIP answered %s", output.ErrUpstream, res.Status)
	}
	loc, err := endpoint.Parse(res.Header.Get("Location"))
	if err != nil || res.Header.Get("Location") == "" || !c.isWHIP(loc) {
		return output.Answer{}, fmt.Errorf("%w: WHIP answered without a usable Location", output.ErrUpstream)
	}
	return output.Answer{SDP: body, Resource: loc.String(), ETag: res.Header.Get("ETag"), Links: res.Header.Values("Link")}, nil
}

// Close deletes a WHIP session resource; one already gone is not an error.
func (c *Client) Close(ctx context.Context, resource string) error {
	u, err := url.Parse(resource)
	if err != nil || !c.isWHIP(u) {
		return errors.New("the stored ingest session is not a resource of the media server")
	}
	res, _, err := c.do(ctx, http.MethodDelete, u.String(), "", nil)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("WHIP DELETE answered %s", res.Status)
	}
	return nil
}

type source struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type path struct {
	Name   string  `json:"name"`
	Source *source `json:"source"`
}

// Publisher is the WebRTC session publishing on program/<id>, "" when none.
func (c *Client) Publisher(ctx context.Context, broadcastID string) (string, error) {
	res, body, err := c.do(ctx, http.MethodGet, c.api.JoinPath("v3/paths/get/program", broadcastID).String(), "", nil)
	if err != nil {
		return "", err
	}
	if res.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("paths/get answered %s", res.Status)
	}
	var p path
	if err := json.Unmarshal(body, &p); err != nil {
		return "", fmt.Errorf("paths/get: %w", err)
	}
	if p.Source == nil || p.Source.Type != webRTCSession {
		return "", nil
	}
	return p.Source.ID, nil
}

// Publishing maps each broadcast id whose program path has a WebRTC publisher to its session.
func (c *Client) Publishing(ctx context.Context) (map[string]string, error) {
	u := c.api.JoinPath("v3/paths/list")
	u.RawQuery = "itemsPerPage=10000"
	res, body, err := c.do(ctx, http.MethodGet, u.String(), "", nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("paths/list answered %s", res.Status)
	}
	var list struct {
		Items []path `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("paths/list: %w", err)
	}
	out := map[string]string{}
	for _, p := range list.Items {
		if id, ok := strings.CutPrefix(p.Name, "program/"); ok && p.Source != nil && p.Source.Type == webRTCSession {
			out[id] = p.Source.ID
		}
	}
	return out, nil
}

// Kick closes a WebRTC session; one already gone is not an error.
func (c *Client) Kick(ctx context.Context, session string) error {
	res, _, err := c.do(ctx, http.MethodPost, c.api.JoinPath("v3/webrtcsessions/kick", session).String(), "", nil)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("webrtcsessions/kick answered %s", res.Status)
	}
	return nil
}

// Ready checks that the control API answers with the internal credentials (readiness).
func (c *Client) Ready(ctx context.Context) error {
	u := c.api.JoinPath("v3/paths/list")
	u.RawQuery = "itemsPerPage=1"
	res, _, err := c.do(ctx, http.MethodGet, u.String(), "", nil)
	if err != nil {
		return errors.New("MediaMTX does not answer")
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("MediaMTX answered %s", res.Status)
	}
	return nil
}

// isWHIP reports whether u is on the WHIP server, so a stored URL can only name its resources.
func (c *Client) isWHIP(u *url.URL) bool {
	return u.Scheme == c.whip.Scheme && u.Host == c.whip.Host && strings.HasPrefix(u.Path, c.whip.Path+"/program/")
}

func (c *Client) do(ctx context.Context, method, target, contentType string, body []byte) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, r)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.secret)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", method, req.URL.Path, unwrapURLError(err))
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, nil, err
	}
	return res, data, nil
}

// unwrapURLError drops the *url.Error wrapper, whose text repeats the full URL.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
