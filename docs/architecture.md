# Architecture & Design

See the main README for the primary Mermaid diagram and explanation.

This document will contain deeper technical details after initial implementation.

## Core Principles (Non-Negotiable)

1. **Capital Protection First** — The RiskManager can veto *any* action.
2. **Full Auditability** — Every decision, research step, and trade is recorded immutably.
3. **Cheap Research First** — RSS + free news APIs before any paid LLM or search call.
4. **Willingness to Do Nothing** — Most cycles will result in PASS. This is a feature.

## Key Components

(See plan.md in the session directory for the original approved design.)
