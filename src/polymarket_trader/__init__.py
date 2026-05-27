"""
Polymarket AI Trading Agent

A profitability-first autonomous trading agent for Polymarket.
Strict capital protection is the #1 priority.

Minimum goal: Never go below starting principal before API budget is exhausted.
Qualified goal: Profits must cover API costs.
Stretch goal: > $20 total profit.
"""

__version__ = "0.1.0"
__all__ = ["__version__"]
