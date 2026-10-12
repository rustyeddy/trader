---
name: review-pr
description: Independently review a Trader pull request using the repository's review rubric, labeling findings and reporting missing evidence. Use when asked to review or check a PR number.
---

Follow the **review-pr** workflow in `docs/workflows/ai-skills.org` and the
rubric in `docs/workflows/ai-review-rubric.org`. Those documents are canonical;
do not restate or improvise their steps here.

The PR number is the user's argument. Two rules that are never skipped:

- Label every finding blocker, suggestion or question, and say what you did not verify; never fabricate verification.
- Do not approve, merge, or push to the branch.
