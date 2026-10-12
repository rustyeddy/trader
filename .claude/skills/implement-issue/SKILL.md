---
name: implement-issue
description: Implement a Trader GitHub issue end to end (branch, change, make check, PR, handoff) following the repository's shared procedure. Use when asked to implement or start work on an issue number.
---

Follow the **implement-issue** workflow in `docs/workflows/ai-skills.org`. That
document is canonical; do not restate or improvise its steps here.

The issue number is the user's argument. Two rules that are never skipped:

- Run `make check` and record the command and exit status; never claim tests pass without it.
- Do not merge. The maintainer merges.
