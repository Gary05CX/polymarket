package price

import (
	"log/slog"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestHandleMessageUsesPriceNotQty(t *testing.T) {
	f := NewFeed("", map[string]string{"btc": "btcusdt"}, 5*time.Second, 10, slog.Default())
	// Real Binance shape: b/a = price, B/A = quantity
	msg := []byte(`{"stream":"btcusdt@bookTicker","data":{"u":1,"s":"BTCUSDT","b":"63874.00000000","B":"3.84107000","a":"63874.01000000","A":"2.48029000"}}`)
	f.handleMessage(msg, map[string]string{"btcusdt": "btc"})

	px, _, ok := f.Get("btc")
	if !ok {
		t.Fatal("expected price")
	}
	// Mid of 63874 and 63874.01 ≈ 63874.005 — NOT ~3.16 (qty mid)
	if px.LessThan(decimal.NewFromInt(60000)) {
		t.Fatalf("got mid %s; likely parsed qty B/A instead of price b/a", px)
	}
	want := decimal.RequireFromString("63874.005")
	if !px.Equal(want) {
		t.Fatalf("mid = %s, want %s", px, want)
	}
}
