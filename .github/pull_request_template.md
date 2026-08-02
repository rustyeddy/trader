## Purpose

<!-- What does this change accomplish, and why is it needed? -->

## Linked issue

<!-- Closes #NNN -->

## Architectural impact

<!--
Which package boundaries does this touch? Does it change dependency
direction, a public contract, or an extension point? Write "None" if the
change is contained within an existing boundary.
-->

## Implementation summary

<!-- What changed, in the order a reviewer should read it. -->

## Testing performed

<!--
Which tests were added or updated, and how they were run. Name the
commands (for example `make check`, `go test -race ./...`). State the
error and boundary cases covered.
-->

## Documentation updated

<!--
Which documents changed: Go doc comments, architecture, package
boundaries, workflows, README, examples. Write "None required" and say
why if no documentation changed.
-->

## ADR impact

<!--
Does this decision belong in `docs/arch/adr-decisions.org`? Name the new
or amended ADR, or state that no durable architectural decision was made.
-->

## Risk and safety considerations

<!--
Live-trading safety, credential handling, data loss, reconciliation, and
concurrency. Confirm real-money trading remains disabled by default.
-->

## Acceptance criteria

- [ ] Linked issue acceptance criteria are satisfied
- [ ] Unit tests were added or updated
- [ ] Integration or contract tests were added where required
- [ ] Race tests were run where concurrency is involved
- [ ] Public Go documentation was updated
- [ ] Architecture documentation was updated where required
- [ ] ADR impact was evaluated
- [ ] README and examples remain accurate
- [ ] No unrelated refactoring is included
- [ ] CI passes
