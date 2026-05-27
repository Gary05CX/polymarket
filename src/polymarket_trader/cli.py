"""
Typer CLI entrypoint for the Polymarket AI Trading Agent.

Commands:
- init: Record capital snapshot + initialize DB
- run: Start the autonomous agent (with --dry-run safety)
- status: Show current equity, open positions, recent decisions, costs
- export: Export decisions/trades for analysis
"""
from __future__ import annotations

import typer
import asyncio

from rich.console import Console
from rich.panel import Panel

from .config import get_settings, reload_settings
from .data.db import init_db, get_current_capital
from .logging import setup_logging

app = typer.Typer(
    name="polymarket-trader",
    help="Profitability-first AI trading agent for Polymarket. Capital protection is #1 priority.",
    add_completion=False,
)
console = Console()


@app.command()
def init(
    force: bool = typer.Option(False, "--force", help="Re-initialize even if snapshot exists"),
) -> None:
    """Initialize database and record the sacred initial capital snapshot."""
    setup_logging()
    settings = reload_settings()

    console.print(Panel.fit(
        "[bold red]⚠️  CAPITAL PROTECTION MODE ENABLED[/bold red]\n\n"
        "This agent will refuse any action that risks going below your starting principal.\n"
        "Start with PAPER_TRADING=true.",
        title="Polymarket AI Trader",
        border_style="red",
    ))

    init_db()
    capital = get_current_capital()

    console.print(f"[green]✓[/green] Database initialized at {settings.duckdb_path}")
    console.print(f"[green]✓[/green] Sacred initial capital recorded: [bold]${capital:.2f}[/bold]")
    console.print("\nNext steps:")
    console.print("  1. Run with: [bold]uv run polymarket-trader run --dry-run[/bold]")
    console.print("  2. Monitor:  [bold]uv run polymarket-trader status[/bold]")


@app.command()
def run(
    dry_run: bool = typer.Option(True, "--dry-run/--live", help="Paper trade only (recommended)"),
    max_iterations: int = typer.Option(3, help="Safety limit for manual test runs"),
) -> None:
    """Run a limited number of autonomous loop iterations (strongly recommended with --dry-run)."""
    setup_logging()
    settings = reload_settings()

    if not dry_run and settings.paper_trading:
        console.print("[red]ERROR:[/red] PAPER_TRADING=true in .env. Refusing live mode.")
        raise typer.Exit(code=1)

    from .agent.loop import TradingAgentLoop

    async def _run() -> None:
        loop = TradingAgentLoop()
        console.print(Panel.fit(
            f"[bold]Agent starting[/bold] — paper_trading={settings.paper_trading}\n"
            f"Will perform up to {max_iterations} safety-limited iterations.",
            border_style="green",
        ))
        for i in range(max_iterations):
            console.print(f"\n[cyan]=== Iteration {i+1}/{max_iterations} ===[/cyan]")
            result = await loop.run_once()
            console.print(result)
            if i < max_iterations - 1:
                await asyncio.sleep(4)  # short demo pause

    import asyncio
    asyncio.run(_run())


@app.command()
def status() -> None:
    """Show current state: capital, recent decisions, P&L, API spend."""
    setup_logging()
    init_db()
    capital = get_current_capital()
    console.print(Panel(f"[bold]Current Protected Capital:[/bold] ${capital:.2f}", style="green"))
    console.print("Full status reporting coming in next implementation phase.")


@app.command()
def export(
    table: str = typer.Argument("decisions", help="decisions | trades | costs"),
    limit: int = 50,
) -> None:
    """Export recent rows from the audit DB as JSON (for analysis / spreadsheets)."""
    console.print(f"Export of {table} (limit {limit}) — placeholder in foundation phase.")


if __name__ == "__main__":
    app()
