---
name: dead-code-removal-policy
description: Policy for removing dead code — includes symbols only referenced by tests
metadata:
  type: feedback
---

If a symbol is only referenced by its own tests and not by any production code path, it should be removed along with its tests.

**Why:** Symbols tested but never called in production are dead weight — they inflate the test surface, create maintenance burden, and signal unfinished design.

**How to apply:** When doing dead code removal, treat test-only references as non-references. Delete the symbol and its corresponding test(s) together.
