// Package github is a quota-safe, caching client for the GitHub REST API.
//
// Safety rules (to avoid bans and quota exhaustion):
//   - Only the official REST API is used; no HTML scraping.
//   - Requests are sequential (never parallel) with at least Delay between them.
//   - X-RateLimit-* headers are read on every response; no new request is sent
//     once the remaining quota drops to Reserve.
//   - A single run may spend at most Budget requests.
//   - Conditional requests use ETags; 304 responses (with a token) do not
//     count against the GitHub quota or the budget.
//   - On 403/429 (rate limit / secondary rate limit) the client stops; it never retries.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const BaseURL = "https://api.github.com"

var (
	ErrBudget  = errors.New("request budget used up")
	ErrReserve = errors.New("GitHub quota reached the safety reserve")
	ErrLimited = errors.New("GitHub rate limit hit")
	ErrMissing = errors.New("not found")
	ErrToken   = errors.New("GitHub token is invalid or unauthorized")
)

// Rate is the core quota state reported by GitHub.
type Rate struct {
	Limit int       `json:"limit"`
	Left  int       `json:"remaining"`
	Reset time.Time `json:"reset"`
	Known bool      `json:"known"`
}

type Client struct {
	Base    string
	Token   string
	HTTP    *http.Client
	Cache   *Cache
	Delay   time.Duration
	Budget  int // max requests that count against the quota in this session
	Reserve int // stop when the remaining quota reaches this

	mu   sync.Mutex
	used int // requests that counted against the quota
	hits int // responses served from cache via 304
	rate Rate
	last time.Time
}

func New(token string, cache *Cache) *Client {
	return &Client{
		Base:    BaseURL,
		Token:   strings.TrimSpace(token),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Cache:   cache,
		Delay:   250 * time.Millisecond,
		Budget:  300,
		Reserve: 10,
	}
}

func (c *Client) Used() int  { c.mu.Lock(); defer c.mu.Unlock(); return c.used }
func (c *Client) Hits() int  { c.mu.Lock(); defer c.mu.Unlock(); return c.hits }
func (c *Client) Rate() Rate { c.mu.Lock(); defer c.mu.Unlock(); return c.rate }

// CanSpend reports whether n more requests fit within the budget and the reserve.
func (c *Client) CanSpend(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used+n > c.Budget {
		return false
	}
	if c.rate.Known && c.rate.Left-n < c.Reserve {
		return false
	}
	return true
}

// FetchRate queries /rate_limit. This call does not count against the quota.
func (c *Client) FetchRate(ctx context.Context) (Rate, error) {
	var body struct {
		Resources struct {
			Core struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"core"`
		} `json:"resources"`
	}
	req, err := c.request(ctx, "/rate_limit", "")
	if err != nil {
		return Rate{}, err
	}
	c.wait()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Rate{}, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Rate{}, ErrToken
	}
	if resp.StatusCode != http.StatusOK {
		return Rate{}, fmt.Errorf("rate_limit: HTTP %d", resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
	if err != nil {
		return Rate{}, err
	}
	core := body.Resources.Core
	r := Rate{Limit: core.Limit, Left: core.Remaining, Reset: time.Unix(core.Reset, 0), Known: true}
	c.mu.Lock()
	c.rate = r
	c.mu.Unlock()
	return r, nil
}

// GetJSON fetches an API path and decodes it into out.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	b, err := c.Get(ctx, path, "application/vnd.github+json")
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// GetRaw fetches file or readme content as raw text.
func (c *Client) GetRaw(ctx context.Context, path string) ([]byte, error) {
	return c.Get(ctx, path, "application/vnd.github.raw+json")
}

// Get sends one GET request with all safety rules applied.
func (c *Client) Get(ctx context.Context, path, accept string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.CanSpend(1) {
		c.mu.Lock()
		over := c.used+1 > c.Budget
		c.mu.Unlock()
		if over {
			return nil, ErrBudget
		}
		return nil, ErrReserve
	}

	key := accept + " " + path
	var hit *Entry
	if c.Cache != nil {
		hit = c.Cache.Get(key)
	}

	req, err := c.request(ctx, path, accept)
	if err != nil {
		return nil, err
	}
	if hit != nil && hit.ETag != "" {
		req.Header.Set("If-None-Match", hit.ETag)
	}

	c.wait()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	c.readRate(resp.Header)

	// 304: unchanged, serve from cache. Free for requests made with a token.
	if resp.StatusCode == http.StatusNotModified && hit != nil {
		c.mu.Lock()
		c.hits++
		if c.Token == "" {
			c.used++ // a 304 without a token may still count; stay on the safe side
		}
		c.mu.Unlock()
		return hit.Body, nil
	}

	c.mu.Lock()
	c.used++
	c.mu.Unlock()

	switch resp.StatusCode {
	case http.StatusOK:
		b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return nil, err
		}
		if etag := resp.Header.Get("ETag"); etag != "" && c.Cache != nil {
			c.Cache.Put(key, &Entry{ETag: etag, Body: b})
		}
		return b, nil
	case http.StatusNotFound, http.StatusConflict:
		// 409: empty repository (no git tree)
		return nil, ErrMissing
	case http.StatusUnauthorized:
		return nil, ErrToken
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, c.limitErr(resp)
	default:
		return nil, fmt.Errorf("GitHub HTTP %d (%s)", resp.StatusCode, path)
	}
}

func (c *Client) limitErr(resp *http.Response) error {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		return fmt.Errorf("%w (retry after %s s)", ErrLimited, ra)
	}
	if r := c.Rate(); r.Known && r.Left == 0 {
		return fmt.Errorf("%w (quota resets at %s)", ErrLimited, r.Reset.Local().Format("15:04"))
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if strings.Contains(strings.ToLower(string(b)), "rate limit") {
		return fmt.Errorf("%w (wait a few minutes)", ErrLimited)
	}
	return fmt.Errorf("GitHub denied access (HTTP %d)", resp.StatusCode)
}

func (c *Client) request(ctx context.Context, path, accept string) (*http.Request, error) {
	url := strings.TrimRight(c.Base, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "SkillScanner/1.0 (+portable desktop tool)")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

// wait keeps at least Delay between requests (sequential, polite use).
func (c *Client) wait() {
	c.mu.Lock()
	d := c.Delay - time.Since(c.last)
	c.mu.Unlock()
	if d > 0 {
		time.Sleep(d)
	}
	c.mu.Lock()
	c.last = time.Now()
	c.mu.Unlock()
}

func (c *Client) readRate(h http.Header) {
	limit, err1 := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	left, err2 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err1 != nil || err2 != nil {
		return
	}
	// Track only the "core" resource.
	if res := h.Get("X-RateLimit-Resource"); res != "" && res != "core" {
		return
	}
	reset, _ := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	c.mu.Lock()
	c.rate = Rate{Limit: limit, Left: left, Reset: time.Unix(reset, 0), Known: true}
	c.mu.Unlock()
}
