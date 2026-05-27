"""
Critical safety tests for RiskManager.

These tests must never be allowed to fail. They encode the capital protection contract.
"""
import pytest
from unittest.mock import patch

from polymarket_trader.agent.risk_manager import RiskManager


@pytest.fixture
def rm(monkeypatch):
    # Force a known safe initial capital for all tests
    with patch("polymarket_trader.agent.risk_manager.get_current_capital", return_value=10.0):
        r = RiskManager()
        r.initial_capital = 10.0
        yield r


def test_never_approve_when_edge_too_small(rm):
    approved, size, reason = rm.approve_trade(edge=0.04, confidence=0.8, recommended_size_usd=1.0)
    assert approved is False
    assert "minimum" in reason.lower()


def test_never_approve_on_low_confidence(rm):
    approved, _, reason = rm.approve_trade(edge=0.12, confidence=0.55, recommended_size_usd=0.8)
    assert approved is False
    assert "confidence" in reason.lower()


def test_approves_only_very_conservative_size(rm):
    approved, size, reason = rm.approve_trade(edge=0.11, confidence=0.78, recommended_size_usd=2.0)
    assert approved is True
    assert size is not None
    assert size <= 0.75   # hard cap from config default + 4% rule on $10


def test_api_budget_exhaustion_blocks_trades(rm):
    with patch("polymarket_trader.agent.risk_manager.CostRepo.total_spent_usd", return_value=9.8):
        approved, _, reason = rm.approve_trade(edge=0.15, confidence=0.85, recommended_size_usd=0.5)
        assert approved is False
        assert "budget" in reason.lower()


def test_blacklist_category_blocks(rm):
    approved, _, reason = rm.approve_trade(
        edge=0.12, confidence=0.8, recommended_size_usd=0.4, market_category="sports"
    )
    assert approved is False
    assert "blacklist" in reason.lower() or "sports" in reason.lower()
