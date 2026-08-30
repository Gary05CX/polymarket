package strategy

import (
	"context"

	"github.com/Gary05CX/polymarket/internal/domain"
)

type Strategy interface {
	ID() string
	Source() string
	Evaluate(ctx context.Context, snap domain.MarketSnapshot) (*domain.Signal, *domain.Reject, error)
}
