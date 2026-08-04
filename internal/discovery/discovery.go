// Package discovery finds active Polymarket 5m/15m crypto up/down markets.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Market is a tradeable up/down window.
type Market struct {
	Slug        string
	ConditionID string
	UpTokenID   string
	DownTokenID string
	Asset       string
	Timeframe   string
	WindowStart int64
	EndTime     time.Time
	EventStart  time.Time
	Title       string
	// Best bid/ask from Gamma (may be zero if missing)
	BestBidUp   decimal.Decimal
	BestAskUp   decimal.Decimal
	BestBidDown decimal.Decimal
	BestAskDown decimal.Decimal
	// Mid prices from outcomePrices when available
	MidUp   decimal.Decimal
	MidDown decimal.Decimal
	Active  bool
}

// Client talks to Gamma API.
type Client struct {
	baseURL string
	http    *http.Client
	grace   time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	market    *Market
	fetchedAt time.Time
}

// New creates a discovery client.
func New(baseURL string, timeout, cacheTTL, prevGrace time.Duration) *Client {
	if baseURL == "" {
		baseURL = "https://gamma-api.polymarket.com"
	}
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		grace:   prevGrace,
		cache:   make(map[string]cacheEntry),
		ttl:     cacheTTL,
	}
}

// DiscoverActive returns markets for all asset×timeframe pairs at now.
func (c *Client) DiscoverActive(ctx context.Context, assets, timeframes []string, now time.Time) ([]Market, error) {
	var out []Market
	var errs []string
	for _, asset := range assets {
		for _, tf := range timeframes {
			m, err := c.DiscoverOne(ctx, asset, tf, now)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s/%s: %v", asset, tf, err))
				continue
			}
			if m != nil {
				out = append(out, *m)
			}
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("discovery failed: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

// DiscoverOne finds a single asset/timeframe market for the current window.
func (c *Client) DiscoverOne(ctx context.Context, asset, timeframe string, now time.Time) (*Market, error) {
	ws, sec, err := WindowStartFor(now, timeframe)
	if err != nil {
		return nil, err
	}
	tf := NormalizeTimeframe(timeframe)
	cacheKey := fmt.Sprintf("%s|%s|%d", strings.ToLower(asset), tf, ws)

	c.mu.Lock()
	if e, ok := c.cache[cacheKey]; ok && time.Since(e.fetchedAt) < c.ttl && e.market != nil {
		m := *e.market
		c.mu.Unlock()
		return &m, nil
	}
	c.mu.Unlock()

	// try current window, then previous near boundary
	starts := []int64{ws}
	elapsed := now.UTC().Unix() - ws
	if c.grace > 0 && elapsed <= int64(c.grace.Seconds()) {
		starts = append(starts, ws-sec)
	}

	var lastErr error
	for _, start := range starts {
		for _, slug := range CandidateSlugs(asset, tf, start) {
			m, err := c.fetchBySlug(ctx, slug, asset, tf, start)
			if err != nil {
				lastErr = err
				continue
			}
			if m == nil {
				continue
			}
			c.mu.Lock()
			c.cache[cacheKey] = cacheEntry{market: m, fetchedAt: time.Now()}
			c.mu.Unlock()
			return m, nil
		}
	}

	// fallback keyword search
	if m, err := c.fallbackSearch(ctx, asset, tf, ws); err == nil && m != nil {
		c.mu.Lock()
		c.cache[cacheKey] = cacheEntry{market: m, fetchedAt: time.Now()}
		c.mu.Unlock()
		return m, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no market for %s %s window %d", asset, tf, ws)
}

func (c *Client) fetchBySlug(ctx context.Context, slug, asset, tf string, windowStart int64) (*Market, error) {
	u := fmt.Sprintf("%s/events?slug=%s", c.baseURL, url.QueryEscape(slug))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma status %d for slug %s", resp.StatusCode, slug)
	}

	// Gamma returns either a JSON array or occasionally a single object.
	var events []gammaEvent
	if err := json.Unmarshal(body, &events); err != nil {
		var one gammaEvent
		if err2 := json.Unmarshal(body, &one); err2 != nil {
			return nil, fmt.Errorf("decode events: %w", err)
		}
		if one.Slug != "" || len(one.Markets) > 0 {
			events = []gammaEvent{one}
		}
	}
	if len(events) == 0 {
		return nil, nil
	}
	ev := events[0]
	if len(ev.Markets) == 0 {
		return nil, nil
	}
	return parseMarket(ev, asset, tf, windowStart)
}

func parseMarket(ev gammaEvent, asset, tf string, windowStart int64) (*Market, error) {
	gm := ev.Markets[0]
	tokenIDs, err := parseStringSlice(gm.ClobTokenIDs)
	if err != nil || len(tokenIDs) < 2 {
		return nil, fmt.Errorf("clobTokenIds missing on %s", ev.Slug)
	}
	outcomes, _ := parseStringSlice(gm.Outcomes)
	prices, _ := parseStringSlice(gm.OutcomePrices)

	upIdx, downIdx := 0, 1
	for i, o := range outcomes {
		switch strings.ToLower(o) {
		case "up", "yes":
			upIdx = i
		case "down", "no":
			downIdx = i
		}
	}
	if upIdx >= len(tokenIDs) || downIdx >= len(tokenIDs) {
		return nil, fmt.Errorf("token index out of range for %s", ev.Slug)
	}

	m := &Market{
		Slug:        firstNonEmpty(ev.Slug, gm.Slug),
		ConditionID: gm.ConditionID,
		UpTokenID:   tokenIDs[upIdx],
		DownTokenID: tokenIDs[downIdx],
		Asset:       strings.ToLower(asset),
		Timeframe:   NormalizeTimeframe(tf),
		WindowStart: windowStart,
		Title:       firstNonEmpty(ev.Title, gm.Question),
		Active:      gm.Active && !gm.Closed,
		BestBidUp:   decimalFromAny(gm.BestBid),
		BestAskUp:   decimalFromAny(gm.BestAsk),
	}
	if t, ok := parseTime(gm.EndDate); ok {
		m.EndTime = t
	} else if t, ok := parseTime(ev.EndDate); ok {
		m.EndTime = t
	}
	if t, ok := parseTime(gm.EventStartTime); ok {
		m.EventStart = t
	} else if t, ok := parseTime(ev.StartTime); ok {
		m.EventStart = t
	}
	if upIdx < len(prices) {
		if d, err := decimal.NewFromString(prices[upIdx]); err == nil {
			m.MidUp = d
		}
	}
	if downIdx < len(prices) {
		if d, err := decimal.NewFromString(prices[downIdx]); err == nil {
			m.MidDown = d
		}
	}
	// if mid missing, approximate from best bid/ask on up (down ~ 1-up)
	if m.MidUp.IsZero() && !m.BestBidUp.IsZero() && !m.BestAskUp.IsZero() {
		m.MidUp = m.BestBidUp.Add(m.BestAskUp).Div(decimal.NewFromInt(2))
	}
	if m.MidDown.IsZero() && !m.MidUp.IsZero() {
		m.MidDown = decimal.NewFromInt(1).Sub(m.MidUp)
	}
	return m, nil
}

func (c *Client) fallbackSearch(ctx context.Context, asset, tf string, windowStart int64) (*Market, error) {
	// public-search is best-effort
	q := fmt.Sprintf("%s up or down %s", asset, tf)
	u := fmt.Sprintf("%s/public-search?q=%s", c.baseURL, url.QueryEscape(q))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	// Try to find events with matching slug prefix
	var payload struct {
		Events []gammaEvent `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		// some responses are bare arrays
		var arr []gammaEvent
		if err2 := json.Unmarshal(body, &arr); err2 != nil {
			return nil, err
		}
		payload.Events = arr
	}
	wantPrefix := fmt.Sprintf("%s-updown-%s-", strings.ToLower(asset), NormalizeTimeframe(tf))
	wantTS := strconv.FormatInt(windowStart, 10)
	for _, ev := range payload.Events {
		slug := strings.ToLower(ev.Slug)
		if strings.Contains(slug, wantPrefix) && strings.Contains(slug, wantTS) {
			return parseMarket(ev, asset, tf, windowStart)
		}
	}
	return nil, fmt.Errorf("fallback search empty")
}

// --- Gamma JSON types (subset) ---

type gammaEvent struct {
	Slug     string        `json:"slug"`
	Title    string        `json:"title"`
	EndDate  string        `json:"endDate"`
	StartTime string       `json:"startTime"`
	Markets  []gammaMarket `json:"markets"`
}

type gammaMarket struct {
	Slug            string          `json:"slug"`
	Question        string          `json:"question"`
	ConditionID     string          `json:"conditionId"`
	ClobTokenIDs    json.RawMessage `json:"clobTokenIds"`
	Outcomes        json.RawMessage `json:"outcomes"`
	OutcomePrices   json.RawMessage `json:"outcomePrices"`
	EndDate         string          `json:"endDate"`
	EventStartTime  string          `json:"eventStartTime"`
	Active          bool            `json:"active"`
	Closed          bool            `json:"closed"`
	BestBid         any             `json:"bestBid"`
	BestAsk         any             `json:"bestAsk"`
}

func parseStringSlice(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("empty")
	}
	// already a JSON array of strings
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	// sometimes a JSON-encoded string containing a JSON array
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999999Z",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func decimalFromAny(v any) decimal.Decimal {
	switch t := v.(type) {
	case nil:
		return decimal.Zero
	case float64:
		return decimal.NewFromFloat(t)
	case string:
		d, err := decimal.NewFromString(t)
		if err != nil {
			return decimal.Zero
		}
		return d
	case json.Number:
		d, err := decimal.NewFromString(string(t))
		if err != nil {
			return decimal.Zero
		}
		return d
	default:
		return decimal.Zero
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
