[繁體中文版](README_zh.md)
# polymarket

**Polymarket AI Trading Agent** — 目前主要開發分支：`feat/polymarket-ai-trading-agent`

這是一個**以盈利為唯一目標**、嚴格保護本金的自主 AI 交易代理。

- 可用資金約 $10 USD
- API 預算約 $10 USD，必須在預算燒完前實現正收益（至少 +$0.01）
- 資料庫：DuckDB（主要）或 PostgreSQL
- 跨平台：Windows / macOS / Linux
- 所有金鑰一律透過 `.env` 管理
- 使用官方 `polymarket-client` SDK 執行交易

**最高優先事項**：保護本金、不低於原始資金。

詳細說明、架構圖、風險規則與快速開始指南請見：
- [README_zh.md](README_zh.md)（中文完整文件，建議先讀）
- `docs/architecture.md`
- 原始設計計劃（開發者）：`.grok/sessions/.../plan.md`

## 快速開始（開發中）

```bash
# 1. 安裝 uv（推薦）或使用 pip
# https://docs.astral.sh/uv/

uv sync --all-extras

# 2. 複製環境檔
cp .env.template .env
# 編輯 .env 填入私鑰與 LLM API Key（強烈建議先用 PAPER_TRADING=true）

# 3. 初始化
uv run polymarket-trader init

# 4. 先用 dry-run 模式跑（最重要！）
uv run polymarket-trader run --dry-run
```

**警告**：這是真實金錢交易。高風險。可能損失全部資金。本專案不構成財務建議。
