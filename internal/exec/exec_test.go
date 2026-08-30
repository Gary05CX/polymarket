package exec

import (
	"testing"

	"github.com/Gary05CX/polymarket/internal/domain"
)

func TestWalkAsks(t *testing.T) {
	asks := []domain.BookLevel{{Price: 0.96, Size: 3}, {Price: 0.97, Size: 10}}
	avg, filled, ok := walkAsks(asks, 5)
	if !ok || filled != 5 {
		t.Fatalf("ok=%v filled=%v", ok, filled)
	}
	want := (3*0.96 + 2*0.97) / 5
	if avg < want-1e-9 || avg > want+1e-9 {
		t.Fatalf("avg %v want %v", avg, want)
	}
	_, _, ok = walkAsks(asks[:1], 5)
	if ok {
		t.Fatal("expected short book to fail")
	}
}
