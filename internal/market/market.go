package market

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gary05CX/polymarket/internal/domain"
)

type Client struct {
	Gamma   string
	Clob    string
	Binance string
	HTTP    *http.Client
	offset  time.Duration
	sync    string
}

func New(gamma, clob, binance, clockSync string) *Client {
	return &Client{
		Gamma:   strings.TrimRight(gamma, "/"),
		Clob:    strings.TrimRight(clob, "/"),
		Binance: strings.TrimRight(binance, "/"),
		HTTP:    &http.Client{Timeout: 10 * time.Second},
		sync:    clockSync,
	}
}

func (c *Client) Now() time.Time { return time.Now().Add(c.offset) }

func (c *Client) SyncClock() error {
	if c.sync == "local" || c.sync == "" {
		c.offset = 0
		return nil
	}
	u := c.Binance + "/api/v3/time"
	b, err := c.get(u)
	if err != nil {
		return err
	}
	var out struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	c.offset = time.UnixMilli(out.ServerTime).Sub(time.Now())
	return nil
}

func WindowStart(t time.Time) time.Time {
	u := t.Unix()
	return time.Unix((u/300)*300, 0).UTC()
}

func (c *Client) FetchMarket(asset string, start time.Time) (domain.Market, error) {
	prefix := strings.ToLower(asset) + "-updown-5m"
	slug := fmt.Sprintf("%s-%d", prefix, start.Unix())
	m, err := c.gammaMarket(slug)
	if err != nil {
		m, err = c.gammaEvent(slug)
		if err != nil {
			return domain.Market{}, err
		}
	}
	m.Asset = strings.ToUpper(asset)
	m.Slug = slug
	m.WindowStart = start.UTC()
	m.WindowEnd = start.Add(5 * time.Minute).UTC()
	if m.MinOrderSize == 0 {
		m.MinOrderSize = 5
	}
	if m.TickSize == 0 {
		m.TickSize = 0.001
	}
	return m, nil
}

func (c *Client) gammaMarket(slug string) (domain.Market, error) {
	m, err := c.gammaMarketQuery(slug, false)
	if err == nil {
		return m, nil
	}
	return c.gammaMarketQuery(slug, true)
}

func (c *Client) gammaMarketQuery(slug string, closed bool) (domain.Market, error) {
	u := c.Gamma + "/markets?slug=" + url.QueryEscape(slug)
	if closed {
		u += "&closed=true"
	}
	b, err := c.get(u)
	if err != nil {
		return domain.Market{}, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return domain.Market{}, err
	}
	if len(arr) == 0 {
		return domain.Market{}, fmt.Errorf("no market %s", slug)
	}
	return parseMarket(arr[0])
}

func (c *Client) gammaEvent(slug string) (domain.Market, error) {
	b, err := c.get(c.Gamma + "/events?slug=" + url.QueryEscape(slug))
	if err != nil {
		return domain.Market{}, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return domain.Market{}, err
	}
	if len(arr) == 0 {
		return domain.Market{}, fmt.Errorf("no event %s", slug)
	}
	mkts, _ := arr[0]["markets"].([]any)
	if len(mkts) == 0 {
		return domain.Market{}, fmt.Errorf("no markets in event %s", slug)
	}
	raw, _ := mkts[0].(map[string]any)
	return parseMarket(raw)
}

func parseMarket(raw map[string]any) (domain.Market, error) {
	m := domain.Market{
		ConditionID:      str(raw["conditionId"]),
		ResolutionSource: str(raw["resolutionSource"]),
		Closed:           asBool(raw["closed"]),
	}
	m.Outcomes = strList(raw["outcomes"])
	tokens := strList(raw["clobTokenIds"])
	if len(tokens) >= 2 {
		if len(m.Outcomes) >= 2 && strings.EqualFold(m.Outcomes[0], "Down") {
			m.DownTokenID, m.UpTokenID = tokens[0], tokens[1]
		} else {
			m.UpTokenID, m.DownTokenID = tokens[0], tokens[1]
		}
	}
	m.OutcomePrices = floatList(raw["outcomePrices"])
	m.MinOrderSize = asFloat(raw["orderMinSize"])
	m.TickSize = asFloat(raw["orderPriceMinTickSize"])
	return m, nil
}

func (c *Client) Mid(token string) (float64, error) {
	b, err := c.get(c.Clob + "/midpoint?token_id=" + url.QueryEscape(token))
	if err != nil {
		return 0, err
	}
	var out struct {
		Mid string `json:"mid"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(out.Mid, 64)
}

func (c *Client) Book(token string) (domain.Book, error) {
	b, err := c.get(c.Clob + "/book?token_id=" + url.QueryEscape(token))
	if err != nil {
		return domain.Book{}, err
	}
	var raw struct {
		Bids         []struct{ Price, Size string } `json:"bids"`
		Asks         []struct{ Price, Size string } `json:"asks"`
		MinOrderSize string                         `json:"min_order_size"`
		TickSize     string                         `json:"tick_size"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return domain.Book{}, err
	}
	bk := domain.Book{MinOrderSize: parseF(raw.MinOrderSize), TickSize: parseF(raw.TickSize)}
	for _, x := range raw.Bids {
		bk.Bids = append(bk.Bids, domain.BookLevel{Price: parseF(x.Price), Size: parseF(x.Size)})
	}
	for _, x := range raw.Asks {
		bk.Asks = append(bk.Asks, domain.BookLevel{Price: parseF(x.Price), Size: parseF(x.Size)})
	}
	sort.Slice(bk.Asks, func(i, j int) bool { return bk.Asks[i].Price < bk.Asks[j].Price })
	sort.Slice(bk.Bids, func(i, j int) bool { return bk.Bids[i].Price > bk.Bids[j].Price })
	return bk, nil
}

func (c *Client) BinancePrice(symbol string) (float64, error) {
	b, err := c.get(c.Binance + "/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0, err
	}
	var out struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(out.Price, 64)
}

func (c *Client) Klines(symbol, interval string, limit int) ([]domain.Candle, error) {
	u := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d", c.Binance, symbol, interval, limit)
	b, err := c.get(u)
	if err != nil {
		return nil, err
	}
	var raw [][]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.Candle, 0, len(raw))
	for _, r := range raw {
		if len(r) < 6 {
			continue
		}
		ms, _ := r[0].(float64)
		out = append(out, domain.Candle{
			OpenTime: time.UnixMilli(int64(ms)).UTC(),
			Open:     parseAny(r[1]),
			High:     parseAny(r[2]),
			Low:      parseAny(r[3]),
			Close:    parseAny(r[4]),
			Volume:   parseAny(r[5]),
		})
	}
	return out, nil
}

func (c *Client) WindowOpenPrice(symbol string, start time.Time) (float64, error) {
	u := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=5m&startTime=%d&limit=1", c.Binance, symbol, start.UnixMilli())
	b, err := c.get(u)
	if err != nil {
		return 0, err
	}
	var raw [][]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return 0, err
	}
	if len(raw) == 0 || len(raw[0]) < 2 {
		return 0, fmt.Errorf("no 5m open")
	}
	return parseAny(raw[0][1]), nil
}

func (c *Client) Snapshot(asset, symbol string, start time.Time) (domain.MarketSnapshot, error) {
	var snap domain.MarketSnapshot
	m, err := c.FetchMarket(asset, start)
	if err != nil {
		return snap, err
	}
	now := c.Now()
	snap.Now = now
	snap.Market = m
	snap.SecondsLeft = m.WindowEnd.Sub(now).Seconds()
	snap.Elapsed = now.Sub(m.WindowStart).Seconds()
	if m.UpTokenID != "" {
		if mid, err := c.Mid(m.UpTokenID); err == nil {
			snap.PMMidUp = mid
		}
	}
	if m.DownTokenID != "" {
		if mid, err := c.Mid(m.DownTokenID); err == nil {
			snap.PMMidDown = mid
		}
	}
	token := m.UpTokenID
	if snap.PMMidDown > snap.PMMidUp {
		token = m.DownTokenID
	}
	if token != "" {
		if bk, err := c.Book(token); err == nil {
			snap.Book = bk
			if bk.MinOrderSize > 0 {
				snap.Market.MinOrderSize = bk.MinOrderSize
			}
			if bk.TickSize > 0 {
				snap.Market.TickSize = bk.TickSize
			}
		}
	}
	if px, err := c.BinancePrice(symbol); err == nil {
		snap.BinancePrice = px
	}
	if open, err := c.WindowOpenPrice(symbol, start); err == nil {
		snap.WindowOpenPx = open
	}
	if k, err := c.Klines(symbol, "1m", 50); err == nil {
		snap.Klines1m = k
	}
	if k, err := c.Klines(symbol, "5m", 8); err == nil {
		snap.Klines5m = k
		if snap.WindowOpenPx == 0 && len(k) > 0 {
			snap.WindowOpenPx = k[len(k)-1].Open
		}
	}
	return snap, nil
}

func (c *Client) Official(slug string) (closed bool, winner string, raw map[string]any, err error) {
	m, err := c.gammaMarket(slug)
	if err != nil {
		m, err = c.gammaEvent(slug)
	}
	if err != nil {
		return false, "", nil, err
	}
	raw = map[string]any{"closed": m.Closed, "outcomes": m.Outcomes, "outcomePrices": m.OutcomePrices}
	if !m.Closed || len(m.OutcomePrices) < 2 || len(m.Outcomes) < 2 {
		return m.Closed, "", raw, nil
	}
	p0, p1 := m.OutcomePrices[0], m.OutcomePrices[1]
	if p0 > 0.95 && p1 < 0.05 {
		return true, m.Outcomes[0], raw, nil
	}
	if p1 > 0.95 && p0 < 0.05 {
		return true, m.Outcomes[1], raw, nil
	}
	return true, "", raw, nil
}

func (c *Client) get(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "polymarket-bot/0.1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, truncate(b, 200))
	}
	return b, nil
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func strList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case string:
		var arr []string
		if json.Unmarshal([]byte(t), &arr) == nil {
			return arr
		}
	}
	return nil
}

func floatList(v any) []float64 {
	switch t := v.(type) {
	case []any:
		out := make([]float64, 0, len(t))
		for _, x := range t {
			out = append(out, asFloat(x))
		}
		return out
	case string:
		var arr []string
		if json.Unmarshal([]byte(t), &arr) == nil {
			out := make([]float64, 0, len(arr))
			for _, s := range arr {
				out = append(out, parseF(s))
			}
			return out
		}
	}
	return nil
}

func parseF(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func parseAny(v any) float64 {
	switch t := v.(type) {
	case string:
		return parseF(t)
	case float64:
		return t
	default:
		return 0
	}
}

func truncate(b []byte, n int) string {
	if len(b) < n {
		return string(b)
	}
	return string(b[:n])
}
