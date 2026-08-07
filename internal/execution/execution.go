// Package execution places and cancels Polymarket CLOB limit orders.
package execution

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/store"
	"github.com/Gary05CX/polymarket/internal/strategy"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderBookMid is optional mid/bid/ask for a token.
type OrderBookMid struct {
	Bid decimal.Decimal
	Ask decimal.Decimal
	Mid decimal.Decimal
}

// CLOB is the trading interface (real or dry-run).
type CLOB interface {
	// GetBookMid returns best bid/ask/mid for a token id.
	GetBookMid(ctx context.Context, tokenID string) (OrderBookMid, error)
	// PlaceLimitBuy posts a GTC limit buy. Returns exchange order id.
	PlaceLimitBuy(ctx context.Context, tokenID string, price, size decimal.Decimal, postOnly bool) (orderID string, err error)
	// CancelOrder cancels by exchange order id.
	CancelOrder(ctx context.Context, orderID string) error
	// CancelAll cancels all open orders for the account.
	CancelAll(ctx context.Context) error
}

// Executor routes signals to CLOB and persists order rows.
type Executor struct {
	cfg  *config.Config
	clob CLOB
	st   *store.Store
	log  *slog.Logger
}

// New builds an executor.
func New(cfg *config.Config, clob CLOB, st *store.Store, log *slog.Logger) *Executor {
	if log == nil {
		log = slog.Default()
	}
	return &Executor{cfg: cfg, clob: clob, st: st, log: log}
}

// PlaceSignal places one signal and records it.
func (e *Executor) PlaceSignal(ctx context.Context, sig strategy.Signal) (*store.Order, error) {
	id := uuid.NewString()
	postOnly := sig.Strategy == "A" // prefer maker on stable strategy

	status := "pending"
	clobID := ""
	var placeErr error

	if e.cfg.CLOB.DryRun {
		status = "dry_run"
		clobID = "dry-" + id[:8]
		e.log.Info("dry_run order",
			"strategy", sig.Strategy,
			"slug", sig.MarketSlug,
			"outcome", sig.Outcome,
			"price", sig.Price.String(),
			"size_usd", sig.SizeUSD.String(),
			"reason", sig.Reason,
		)
	} else {
		oid, err := e.clob.PlaceLimitBuy(ctx, sig.TokenID, sig.Price, sig.Size, postOnly)
		if err != nil {
			placeErr = err
			status = "error"
			e.log.Error("place order failed", "err", err, "slug", sig.MarketSlug, "strategy", sig.Strategy)
		} else {
			clobID = oid
			status = "live"
			e.log.Info("order placed",
				"clob_order_id", oid,
				"strategy", sig.Strategy,
				"slug", sig.MarketSlug,
				"price", sig.Price.String(),
				"size", sig.Size.String(),
			)
		}
	}

	errMsg := ""
	if placeErr != nil {
		errMsg = placeErr.Error()
	}
	o := store.Order{
		ID:           id,
		MarketSlug:   sig.MarketSlug,
		TokenID:      sig.TokenID,
		Strategy:     sig.Strategy,
		Side:         "BUY",
		Price:        sig.Price.String(),
		Size:         sig.Size.String(),
		SizeUSD:      sig.SizeUSD.String(),
		Status:       status,
		CLOBOrderID:  clobID,
		Reason:       sig.Reason,
		DryRun:       e.cfg.CLOB.DryRun,
		ErrorMessage: errMsg,
	}
	if err := e.st.InsertOrder(ctx, o); err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}
	// On successful/dry buy we optimistically track position for risk caps.
	if status == "live" || status == "dry_run" {
		_ = e.st.UpsertPosition(ctx, sig.MarketSlug, sig.TokenID, sig.Size, sig.Price, sig.SizeUSD)
	}
	if placeErr != nil {
		return &o, placeErr
	}
	return &o, nil
}

// CancelOpen cancels live exchange orders. Dry-run fills are left for settlement.
func (e *Executor) CancelOpen(ctx context.Context) error {
	orders, err := e.st.ListOpenOrders(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, o := range orders {
		// Keep dry_run rows so paper settlement can still resolve them.
		if o.Status == "dry_run" || o.Status == "dry_filled" {
			continue
		}
		if e.cfg.CLOB.DryRun || o.CLOBOrderID == "" {
			_ = e.st.UpdateOrderStatus(ctx, o.ID, "cancelled", "")
			continue
		}
		if err := e.clob.CancelOrder(ctx, o.CLOBOrderID); err != nil {
			e.log.Warn("cancel order failed", "id", o.ID, "clob", o.CLOBOrderID, "err", err)
			if first == nil {
				first = err
			}
			continue
		}
		_ = e.st.UpdateOrderStatus(ctx, o.ID, "cancelled", o.CLOBOrderID)
	}
	if !e.cfg.CLOB.DryRun {
		if err := e.clob.CancelAll(ctx); err != nil {
			e.log.Warn("cancel all failed", "err", err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

// RefreshMids loads book mids for up/down tokens.
func (e *Executor) RefreshMids(ctx context.Context, upToken, downToken string) (up, down OrderBookMid, err error) {
	if e.clob == nil {
		return OrderBookMid{}, OrderBookMid{}, fmt.Errorf("clob nil")
	}
	up, err = e.clob.GetBookMid(ctx, upToken)
	if err != nil {
		return
	}
	down, err = e.clob.GetBookMid(ctx, downToken)
	return
}
