"""
Central configuration using pydantic-settings + .env.

All sensitive values and tunable parameters live here.
Never import from here directly in hot paths — pass Settings instance.
"""
from __future__ import annotations

from pathlib import Path
from typing import Literal

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
    )

    # === Polymarket / Wallet ===
    polymarket_private_key: str = Field(..., alias="POLYMARKET_PRIVATE_KEY")
    polymarket_deposit_wallet_address: str | None = Field(
        None, alias="POLYMARKET_DEPOSIT_WALLET_ADDRESS"
    )

    # === LLM ===
    xai_api_key: str | None = Field(None, alias="XAI_API_KEY")
    openai_api_key: str | None = Field(None, alias="OPENAI_API_KEY")
    anthropic_api_key: str | None = Field(None, alias="ANTHROPIC_API_KEY")

    reasoning_model: str = Field("grok-4-fast", alias="REASONING_MODEL")
    fast_model: str = Field("gpt-4o-mini", alias="FAST_MODEL")

    # === Research (optional) ===
    thenewsapi_api_key: str | None = Field(None, alias="THENEWSAPI_API_KEY")
    gnews_api_key: str | None = Field(None, alias="GNEWS_API_KEY")
    newsdata_api_key: str | None = Field(None, alias="NEWSDATA_API_KEY")

    # === Safety & Capital Protection (SACRED) ===
    paper_trading: bool = Field(True, alias="PAPER_TRADING")
    initial_capital_usd: float = Field(10.0, alias="INITIAL_CAPITAL_USD")
    max_api_budget_usd: float = Field(10.0, alias="MAX_API_BUDGET_USD")
    min_edge_percent: float = Field(9.5, alias="MIN_EDGE_PERCENT")
    max_risk_per_trade_usd: float = Field(0.75, alias="MAX_RISK_PER_TRADE_USD")

    # === Runtime ===
    poll_interval_seconds: int = Field(600, alias="POLL_INTERVAL_SECONDS")
    require_trade_confirmation: bool = Field(True, alias="REQUIRE_TRADE_CONFIRMATION")
    max_llm_calls_per_cycle: int = Field(6, alias="MAX_LLM_CALLS_PER_CYCLE")

    # === DB ===
    duckdb_path: str = Field("data/trader.duckdb", alias="DUCKDB_PATH")
    # Future: db_backend: Literal["duckdb", "postgres"] = "duckdb"

    log_level: Literal["DEBUG", "INFO", "WARNING", "ERROR"] = Field("INFO", alias="LOG_LEVEL")
    market_category_blacklist: list[str] = Field(
        default_factory=lambda: ["sports", "entertainment"],
        alias="MARKET_CATEGORY_BLACKLIST",
    )

    @field_validator("polymarket_private_key")
    @classmethod
    def validate_private_key(cls, v: str) -> str:
        if not v or not v.startswith("0x") or len(v) < 60:
            raise ValueError("POLYMARKET_PRIVATE_KEY must be a valid 0x... private key")
        return v

    @property
    def data_dir(self) -> Path:
        return Path(self.duckdb_path).parent

    @property
    def is_paper_trading(self) -> bool:
        return self.paper_trading


# Singleton accessor (simple for v1)
_settings: Settings | None = None


def get_settings() -> Settings:
    global _settings
    if _settings is None:
        _settings = Settings()  # type: ignore[arg-type]
    return _settings


def reload_settings() -> Settings:
    """Useful in tests."""
    global _settings
    _settings = Settings()  # type: ignore[arg-type]
    return _settings
