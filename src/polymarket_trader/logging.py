"""
Structured logging setup using structlog + rich for console.

All decisions and important events are also persisted to the DB (append-only).
"""
from __future__ import annotations

import logging
import sys
from pathlib import Path
from typing import Any

import structlog
from rich.console import Console
from rich.logging import RichHandler

from .config import get_settings


def setup_logging() -> None:
    """Configure structlog + rich. Call once at startup."""
    settings = get_settings()

    # Ensure logs dir exists
    log_dir = Path("logs")
    log_dir.mkdir(exist_ok=True)

    # Rich console handler (beautiful terminal output)
    console = Console()
    rich_handler = RichHandler(
        console=console,
        show_time=True,
        show_path=False,
        rich_tracebacks=True,
        markup=True,
    )
    rich_handler.setLevel(getattr(logging, settings.log_level))

    # File handler for structured JSONL (machine readable audit trail)
    file_handler = logging.FileHandler(log_dir / "agent.jsonl", encoding="utf-8")
    file_handler.setLevel(logging.INFO)

    logging.basicConfig(
        level=getattr(logging, settings.log_level),
        format="%(message)s",
        handlers=[rich_handler, file_handler],
        force=True,
    )

    # structlog configuration
    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            structlog.stdlib.filter_by_level,
            structlog.stdlib.add_logger_name,
            structlog.stdlib.add_log_level,
            structlog.stdlib.PositionalArgumentsFormatter(),
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.UnicodeDecoder(),
            # Final renderer: JSON for files + pretty for console (RichHandler handles console)
            structlog.processors.JSONRenderer() if not sys.stderr.isatty() else structlog.dev.ConsoleRenderer(colors=True),
        ],
        context_class=dict,
        logger_factory=structlog.stdlib.LoggerFactory(),
        wrapper_class=structlog.stdlib.BoundLogger,
        cache_logger_on_first_use=True,
    )


def get_logger(name: str) -> structlog.stdlib.BoundLogger:
    """Get a structured logger."""
    return structlog.get_logger(name)
