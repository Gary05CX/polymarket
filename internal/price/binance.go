// Package price provides low-latency Binance spot prices via WebSocket.
package price

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
)

// Feed maintains last mid prices from Binance bookTicker streams.
type Feed struct {
	wsBase  string
	symbols map[string]string // asset -> binance symbol e.g. btc -> btcusdt
	stale   time.Duration
	log     *slog.Logger

	mu      sync.RWMutex
	last    map[string]quote // key: asset lowercase
	history map[string][]sample
	histN   int

	cancel context.CancelFunc
	done   chan struct{}
}

type quote struct {
	price decimal.Decimal
	at    time.Time
}

type sample struct {
	price decimal.Decimal
	at    time.Time
}

// NewFeed builds a multi-asset Binance price feed.
func NewFeed(wsBase string, symbols map[string]string, stale time.Duration, histN int, log *slog.Logger) *Feed {
	if wsBase == "" {
		wsBase = "wss://stream.binance.com:9443/ws"
	}
	if stale <= 0 {
		stale = 5 * time.Second
	}
	if histN <= 0 {
		histN = 60
	}
	if log == nil {
		log = slog.Default()
	}
	// normalize
	norm := make(map[string]string, len(symbols))
	for a, s := range symbols {
		norm[strings.ToLower(a)] = strings.ToLower(s)
	}
	return &Feed{
		wsBase:  strings.TrimRight(wsBase, "/"),
		symbols: norm,
		stale:   stale,
		log:     log,
		last:    make(map[string]quote),
		history: make(map[string][]sample),
		histN:   histN,
		done:    make(chan struct{}),
	}
}

// Start connects and streams until ctx cancelled.
func (f *Feed) Start(ctx context.Context) error {
	if len(f.symbols) == 0 {
		return fmt.Errorf("no binance symbols configured")
	}
	ctx, f.cancel = context.WithCancel(ctx)
	go f.loop(ctx)
	return nil
}

// Stop cancels the feed.
func (f *Feed) Stop() {
	if f.cancel != nil {
		f.cancel()
	}
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
	}
}

// Get returns last mid for asset if fresh enough.
func (f *Feed) Get(asset string) (decimal.Decimal, time.Duration, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	q, ok := f.last[strings.ToLower(asset)]
	if !ok || q.price.IsZero() {
		return decimal.Zero, 0, false
	}
	age := time.Since(q.at)
	if age > f.stale {
		return q.price, age, false
	}
	return q.price, age, true
}

// RealizedSigma estimates short-horizon vol from log returns (absolute mean as crude σ).
func (f *Feed) RealizedSigma(asset string) decimal.Decimal {
	f.mu.RLock()
	defer f.mu.RUnlock()
	hist := f.history[strings.ToLower(asset)]
	if len(hist) < 3 {
		return decimal.Zero
	}
	// mean absolute log-return between consecutive samples
	sum := decimal.Zero
	n := 0
	for i := 1; i < len(hist); i++ {
		if hist[i-1].price.IsZero() || hist[i].price.IsZero() {
			continue
		}
		// |p1/p0 - 1| approx log return for small moves
		r := hist[i].price.Div(hist[i-1].price).Sub(decimal.NewFromInt(1)).Abs()
		sum = sum.Add(r)
		n++
	}
	if n == 0 {
		return decimal.Zero
	}
	return sum.Div(decimal.NewFromInt(int64(n)))
}

func (f *Feed) loop(ctx context.Context) {
	defer close(f.done)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := f.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		f.log.Warn("binance ws disconnected", "err", err, "reconnect_in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (f *Feed) connectOnce(ctx context.Context) error {
	// combined stream: /stream?streams=btcusdt@bookTicker/ethusdt@bookTicker
	streams := make([]string, 0, len(f.symbols))
	symbolToAsset := make(map[string]string, len(f.symbols))
	for asset, sym := range f.symbols {
		streams = append(streams, sym+"@bookTicker")
		symbolToAsset[sym] = asset
	}
	// use combined stream endpoint
	base := f.wsBase
	// if ends with /ws, switch to /stream for multi
	u := base
	if strings.HasSuffix(base, "/ws") {
		u = strings.TrimSuffix(base, "/ws") + "/stream?streams=" + strings.Join(streams, "/")
	} else {
		u = base + "/stream?streams=" + strings.Join(streams, "/")
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, u, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	f.log.Info("binance ws connected", "url", u)

	// reset backoff success path is in loop
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	})

	// ping ticker
	pingDone := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				close(pingDone)
				return
			case <-pingDone:
				return
			case <-t.C:
				_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
			}
		}
	}()
	defer close(pingDone)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		f.handleMessage(data, symbolToAsset)
	}
}

type combinedMsg struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type bookTicker struct {
	Symbol string `json:"s"`
	Bid    string `json:"b"`
	Ask    string `json:"a"`
}

func (f *Feed) handleMessage(data []byte, symbolToAsset map[string]string) {
	var wrap combinedMsg
	if err := json.Unmarshal(data, &wrap); err != nil {
		return
	}
	payload := wrap.Data
	if len(payload) == 0 {
		payload = data
	}
	var bt bookTicker
	if err := json.Unmarshal(payload, &bt); err != nil {
		return
	}
	sym := strings.ToLower(bt.Symbol)
	asset, ok := symbolToAsset[sym]
	if !ok {
		// try match by stream name
		for s, a := range symbolToAsset {
			if strings.Contains(strings.ToLower(wrap.Stream), s) {
				asset = a
				ok = true
				break
			}
		}
	}
	if !ok {
		return
	}
	bid, err1 := decimal.NewFromString(bt.Bid)
	ask, err2 := decimal.NewFromString(bt.Ask)
	if err1 != nil || err2 != nil || bid.IsZero() || ask.IsZero() {
		return
	}
	mid := bid.Add(ask).Div(decimal.NewFromInt(2))
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[asset] = quote{price: mid, at: now}
	h := append(f.history[asset], sample{price: mid, at: now})
	if len(h) > f.histN {
		h = h[len(h)-f.histN:]
	}
	f.history[asset] = h
}
