# 5min crypto — strategy notes (from manual play)

## Hard limits (loss avoidance)

- Stake **$1 per trade** (default).
- **Never** size a single order **> $5**. Large size feels easy to get flipped / adverse-selected on thin books.
- Cumulative **3 losses → stop** the program.
- **At most one entry per 5-minute window** (once we buy, stop hunting in that window).

## Buy rule

1. Identify the current 5-minute crypto market window.
2. **Do not trade** during the first **3 minutes** of the window (minutes 0–3 exclusive of the watch phase).
3. **From minute 3 until the window ends** (last ~2 minutes): poll the crowd price on a **short interval** (target **1–2s**; REST is fine at this rate).
4. On each poll: read Up and Down market prices (crowd / book mid or best ask for the side we would buy).
5. If **either side ≥ 70%** (price ≥ 0.70): **follow that majority** and buy **$1** of that side **immediately**, then stop watching for entries this window.
6. If the window ends with **neither** side ever ≥ 70%: **skip** (no trade).
7. After resolution: log win/loss. On loss, increment loss flag; stop the program at **3** losses.

### Notes on “majority”

- Majority = Polymarket **market price** for Up or Down (where the crowd is standing), not personal conviction and not a larger size.
- Prefer a single consistent quote source (e.g. mid, or best ask if we marketably buy). Pin this when implementing.
- 40/60-type boards are **skip** until/unless one side actually hits 70%.

## Why small size

- Goal is **not** to move the book or “win big”; it’s to stand with the crowd in small clips.
- $1 clips keep max pain per mistake tiny; cap $5 is a hard ceiling if we ever raise size.

## CLOB: REST vs WebSocket (for this bot)

CLOB = Polymarket’s **Central Limit Order Book** service (`clob.polymarket.com`). It is **not** “WS only”:

| Path | What it is | Use for us |
| --- | --- | --- |
| **CLOB REST** | HTTP endpoints: `/price`, `/midpoint`, `/book`, `POST /order`, … | What you’ve already tried. Enough for 1–2s polls + placing the $1 order. |
| **CLOB WebSocket** | Separate stream: `wss://ws-subscriptions-clob.polymarket.com/ws/market` (and `.../ws/user` for your fills) | Push updates (`best_bid_ask`, `price_change`, book, trades). Better for **tight tracking** without hammering REST. |

- **Finding the market** (which 5m event/token IDs): usually **Gamma API** (REST), not CLOB.
- **Watching 70% in the last 2 minutes**: **WS market channel is better** (lower latency, less poll noise). **REST poll every 1–2s is still OK** and well under rate limits.
- **Actually buying $1**: still **CLOB REST** (signed `POST /order`) in the usual flow; user WS is optional for fill confirmation.

**Practical plan:** v1 can be all REST (Gamma + CLOB poll + CLOB order). v2 add market WS for the post–minute-3 watch loop if we want snappier triggers.

## Open for implementation

- [ ] Paper / dry-run first vs live CLOB orders
- [ ] Which series (BTC / ETH / … 5m)
- [ ] Exact quote field: mid vs best ask
- [ ] Poll interval default (1s vs 2s) if staying on REST
