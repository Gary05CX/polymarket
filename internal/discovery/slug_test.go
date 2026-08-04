package discovery

import (
	"testing"
	"time"
)

func TestWindowStart5m(t *testing.T) {
	// 2026-08-04 04:27:30 UTC → floor to 04:25:00
	ts := time.Date(2026, 8, 4, 4, 27, 30, 0, time.UTC)
	got := WindowStart(ts, 300)
	want := time.Date(2026, 8, 4, 4, 25, 0, 0, time.UTC).Unix()
	if got != want {
		t.Fatalf("WindowStart 5m: got %d want %d", got, want)
	}
}

func TestWindowStart15m(t *testing.T) {
	ts := time.Date(2026, 8, 4, 4, 37, 0, 0, time.UTC)
	got := WindowStart(ts, 900)
	want := time.Date(2026, 8, 4, 4, 30, 0, 0, time.UTC).Unix()
	if got != want {
		t.Fatalf("WindowStart 15m: got %d want %d", got, want)
	}
}

func TestCandidateSlugsPrimary(t *testing.T) {
	slugs := CandidateSlugs("btc", "5m", 1785817500)
	if len(slugs) == 0 {
		t.Fatal("empty slugs")
	}
	if slugs[0] != "btc-updown-5m-1785817500" {
		t.Fatalf("primary slug = %s", slugs[0])
	}
	slugs15 := CandidateSlugs("btc", "15m", 1785817800)
	if slugs15[0] != "btc-updown-15m-1785817800" {
		t.Fatalf("primary 15m slug = %s", slugs15[0])
	}
}
