# Documentation index

## Current

| Document | What it is |
|---|---|
| [User's Guide](UsersGuide.md) | the commands, configuration and data layout |
| [Developer's Guide](DevelopersGuide.md) | packages, layering, the market-data pipeline, tests |
| [arch/package-boundaries.org](arch/package-boundaries.org) | the live packages and what each may import |
| [arch/adr-decisions.org](arch/adr-decisions.org) | the registry of architecture decisions (ADR-089 to ADR-091 describe the current market-data structure) |
| [arch/market-data-storage.md](arch/market-data-storage.md) | the data path convention |
| [CONTRIBUTING.org](CONTRIBUTING.org) | issue, branch, pull request and merge process |
| [workflows/](workflows) | testing, AI-assisted development, review rubric, Go testing rules |
| [../AGENTS.md](../AGENTS.md) | principles, layering, security and working rules for agents and contributors |

## Background and history

These describe how the design was reached or the earlier, larger runtime. They are
kept for their reasoning; they may name packages, commands or paths that no longer
exist (the earlier code is in [`../_archive`](../_archive)).

- [trader.org](trader.org), [architecture.org](architecture.org): the north-star
  documents of the design phase
- [arch/trader-framework-architecture.org](arch/trader-framework-architecture.org),
  [arch/trader-framework-requirements.org](arch/trader-framework-requirements.org)
- [todo.org](todo.org): the design-phase task list
- [milestones/](milestones), [plans/](plans), [reviews/](reviews): completion
  reviews and plans of earlier milestones
- [research/](research): strategy research notes and results
- ADRs about archived code (Alpaca, Stooq, MCP, the earlier backtest services):
  see the note at the top of the [ADR registry](arch/adr-decisions.org)
- [`../_archive/docs`](../_archive/docs): the earlier User's and Developer's Guides
  and package-boundary matrix
