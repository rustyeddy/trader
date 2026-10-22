# Trader Agent Pipeline Automation Plan

Oct 14, 2026 · @Rusty

The seven-step flow can run on GitHub Actions with labels as the state machine, with three changes: GitHub merges instead of Claude, you gate risky plans, and a different model reviews.

## Design changes

Three changes to the proposed seven-step flow make it safer without slowing it down.

### 1. GitHub merges, not Claude

The docs already say an assistant never merges its own PR. Instead, Claude enables auto-merge (`gh pr merge --auto --squash`) when it opens the PR, and branch protection on `main` requires:

- your approval, via CODEOWNERS
- the `ci` and `ai-review` checks passing
- stale approvals dismissed on new commits
- all review conversations resolved

GitHub merges once all of that holds. Your approval is the merge trigger, so Claude never holds merge authority and step 7 shrinks to addressing comments. The older `claude-gpt-copilot-pipeline-plan.org` auto-merged on AI approvals alone; the human gate in step 6 fixes that.

### 2. You gate risky plans

If an AI approves every plan, your first look is a finished PR, and a wrong plan costs a full implement-and-review cycle. The AI may approve plans for docs and contained code. Plans touching ADRs, package boundaries, concurrency, or the order/risk path get `plan:needs-maintainer` and wait for you.

### 3. A different model reviews

Claude reviewing Claude is the weakest independence; the #490 pilot showed the value comes from a different assistant. The `ai-review` job calls GPT with `ai-review-rubric.org`, posts the review, and sets an `ai-review` check that fails on any Blocker. Copilot can run alongside, but as far as known its reviews only comment, so it cannot be the gate.

## The pipeline

&#91;embedded content: issue-to-merge pipeline · 2 human gates, 2 loops\]

A weak plan loops back to Claude, a risky one waits for you, and every push after review re-runs CI and the AI review before GitHub can merge.

## Stages

Each job's prompt is one line pointing at a procedure in `ai-skills.org` ("Follow implement-issue for #N"), never restated policy, so there is one copy to keep in sync.

| Step | Trigger | Job | Result |
| --- | --- | --- | --- |
| 1 | You add `agent:plan` to an issue, on GitHub or with `gh issue edit N --add-label agent:plan` | `claude-plan` | Plan comment from the implementation checklist; label `plan:review` |
| 2–3 | `plan:review` label | `plan-review` (GPT) | Review comment, then `plan:approved`, `plan:revise` (re-runs the plan step), or `plan:needs-maintainer` |
| 4 | `plan:approved` label | `claude-implement` | Branch, code, `make check`, PR with `Closes #N`, handoff comment, auto-merge enabled |
| 5 | PR opened or updated | `ai-review` (GPT) | Review comments; `ai-review` check passes or fails |
| 6 | You | — | Approve, or comment |
| 7 | Review submitted with changes requested or comments | `claude-address` | Fixes pushed, each comment answered with its commit; the push re-runs CI and AI review |
| Merge | All branch-protection rules satisfied | GitHub | Merged; issue closes |

`ai-skills.org` needs two new procedures: **plan-review** and **address-review**.

## Gotchas

Seven things will break the pipeline or make it unsafe if missed.

| Risk | What happens | Fix |
| --- | --- | --- |
| Your identity | If Claude pushes with your token, PRs are authored by you, and GitHub won't let you approve your own PR | Claude acts as a GitHub App, such as the Claude app from `/install-github-app` |
| `GITHUB_TOKEN` chaining | Labels and pushes made with it don't start other workflows, so the chain stalls after step 1 | Label-setting and pushing jobs use a GitHub App token via `actions/create-github-app-token` |
| Public repo | Anyone can label or comment, spend your API budget, and steer Claude | Every label- or comment-triggered job checks `github.actor == 'rustyeddy'` |
| Endless loops | AI reviewer and Claude bounce forever | Count fix rounds; after three, label `needs-human` and stop |
| Branch ownership | Two jobs push to one branch at once | `concurrency: issue-${{ number }}` with `cancel-in-progress: false` |
| Secrets | Broker credentials leak into Actions | None in the Actions environment; ADR-078's env-only rule makes this checkable |
| Policy docs | `ai-development.org` says no automated orchestration; `ai-skills.org` defers it | Treat adoption as a policy change: its own issue, and un-mark the pipeline plan as exploratory |

## Rollout

Build in three stages, and run two or three real feature issues through each before adding the next.

1. **Branch protection, auto-merge, and the `ai-review` check.** Covers steps 5, 6 and the merge: the most value for the least risk.
2. **`claude-address`.** Claude responds to reviews automatically.
3. **The plan stage, steps 1–3.** The trickiest part, and the one most worth watching.

The last 11 merges (#503–#518) are product work, so this is a good time to start.

- [ ] Open an issue for the policy change and stage 1
- [ ] Grant push access if Claude should draft the stage 1 PR (workflows, CODEOWNERS, branch-protection settings, doc updates)
- [ ] Check the action's input names against the Claude Code GitHub Actions docs before writing YAML

## Sources

- [GitHub Docs: Triggering a workflow](https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/triggering-a-workflow) (the `GITHUB_TOKEN` limitation)
- [Claude Code GitHub Actions](https://code.claude.com/docs/en/github-actions) (not opened for this plan; verify input names)
