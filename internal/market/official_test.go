package market

import "testing"

func TestOfficialClosedMarket(t *testing.T) {
	c := New("https://gamma-api.polymarket.com", "https://clob.polymarket.com", "https://api.binance.com", "local")
	closed, winner, _, err := c.Official("btc-updown-5m-1787312700")
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("expected closed")
	}
	if winner != "Down" {
		t.Fatalf("winner %q", winner)
	}
}
