# Foundation Unused Generation Cleanup

Executor: Sol. Scope prepared 2026-10-09 from `890b9e0`.

This scope applies the architecture's [unused-code rule](../architecture.md#9-migration-and-evidence).
The [deputy gateway batch](foundation-deputy-gateway.md#release-verification) is released.
This cleanup does not move packages or change a product decision.

## Verified Paths

| Code | Current consumers | Required treatment |
|---|---|---|
| `cardInstructions` in `status_build.go` | None. Repository search finds only its declaration. | Remove the unused instructions. Do not register an unused prompt. |
| `statusGenerate` in `status_build.go` | None. The architecture test names its declaration as a migration exception. | Remove the unused generation path and its exception. |
| `ProcessCard` in `status_build.go` | Application worker registration and upgrade contracts. | Keep its fenced acknowledgement of old queue work. |
| Card types and scheduling helpers | Current-state workflows and existing contracts. | Keep their existing consumers and behavior. |
| Legacy answer and routing HTTP interfaces | Registered HTTP handlers and tests; routing also has a settings control. | Keep them outside this cleanup. Their support or retirement needs a separate decision. |

## Scope and Order

1. Recheck declarations, callers, worker registration, and current-state contracts.
2. Remove only the unused instructions and generation method.
3. Remove only the matching provider-boundary exception.
4. Verify current-state scheduling and frozen cards with existing contracts. Compare the unchanged old queue handler with the baseline.
5. Run required checks and complete CI before direct integration.
6. Back up, release, and check the real default Codex secretary.

Do not delete database rows, migrations, card types, or compatibility handlers.
Do not change model input, output schemas, retries, budgets, or current-state scheduling.
Do not add tests that merely require deleted identifiers to remain absent.
The existing architecture test guards provider entry points. Existing behavior contracts guard the surviving workflows.

## Acceptance

- No production or test caller needs either removed declaration.
- The architecture test no longer permits the removed provider path.
- Current-state scheduling and old card acknowledgement preserve their existing behavior.
- Formatting, repository checks, pgvector integration checks, and complete CI pass.
- Release backup and live default Codex checks pass, including own-request cleanup and claim counts.

Record filtered checks as filtered. Keep failed checks and their reasons.
This scope does not complete Phase 3.9 or establish complete gateway coverage.

## Status

The unused instructions, generation method, and matching architecture exception are removed in the executor branch.
The old queue handler's complete bytes match the baseline.
Formatting, static analysis, service build, filtered repository checks, and filtered pgvector integration checks pass.
The selected contracts cover current-state writes, frozen cards, scheduling without a model, timeline reads, and background contention.
The document checker passes index coverage, links, anchors, sentence length, and paragraph limits.
Complete CI, direct integration, backup, release, and live checks remain outstanding.

## Sol Window Prompt

```text
Window: Sol
Continue Phase 3.9 under docs/tasks/foundation-unused-generation.md.
Read AGENTS.md, this scope, and its current specification links first.
Use your isolated worktree. Do not switch or stage /root/PCAS.
Use Chinese for user replies and English for code, commits, and current documents.
Recheck the callers of cardInstructions and statusGenerate before removing them.
Remove only those unused declarations and the matching architecture exception.
Keep card data, migrations, current-state helpers, and ProcessCard.
Keep legacy HTTP interfaces outside this scope.
Run existing scheduling and frozen-card contracts with pgvector.
Compare the old queue handler with the baseline and keep its existing acknowledgement.
Run required checks and complete CI. Report every filtered or missing check.
Integrate directly after clean review and passing CI. Do not create a PR.
Back up before release and check the actual default Codex secretary after release.
Clean only your smoke, request, or source identities. Check claim counts afterward.
Do not move packages, add model wrappers, change prompts in use, or add a migration.
Ask only when an actual stop condition in AGENTS.md applies.
```
