package settle

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPnLWinLose(t *testing.T) {
	// size 10 @ 0.60 → cost 6; win pays 10 → pnl +4
	size := decimal.NewFromInt(10)
	cost := decimal.NewFromInt(6)
	winPnL := size.Sub(cost)
	if !winPnL.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("win pnl %s", winPnL)
	}
	losePnL := cost.Neg()
	if !losePnL.Equal(decimal.NewFromInt(-6)) {
		t.Fatalf("lose pnl %s", losePnL)
	}
}
