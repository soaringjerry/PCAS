# UX regression repairs after PR #1

Base: `e48aa29c68d05e3250c911ec846cff4f6430164e`.

- Hall replies now have opaque server-owned turn IDs. Follow-ups load the owner's chosen assistant's stored question/answer and verify all input memory versions, current source support and grants. Browser-supplied answers are ignored. A revoked or superseded turn is withheld while unrelated ordinary history remains usable. Deletion removes affected stored replies.
- A clear Hall delegation atomically creates its task and queues the configured assistant, within the existing execution, reservation and worker boundaries. Delegating an answer card carries server turn IDs and rechecks the stored discussion dependencies before including its answers. The ordinary workspace request receipt deduplicates network retries; the stable task ID rejects a replay under a new request ID. Ambiguous discussion asks inline. Missing direct model setup creates no task. Runs continue to produce drafts and suggestions, without new external execution powers.
- Migration 015 introduces authored/derived ownership blocks for adopted fields. Edits carry run dependencies into replacement ranges, insertions inside generated blocks remain derived, and promotion copies the blocks. Revocation filters only denied blocks; deletion removes only affected blocks. Generated task titles no longer erase independently authored notes or checks. Large edits use a bounded conservative fallback, which can protect additional text in the edited range.
- Pre-block mixed fields cannot be reliably split after historical edits. They remain protected for provider use; ambiguous text is quarantined on deletion for explicit owner review, with a visible item notice and an owner-only recovery endpoint. Quarantine is excluded from model context and ordinary export. It may contain both authored and generated legacy text; it is not a claim that all legacy sensitive bytes can be erased while automatically recovering unknown authorship.
- Queued runs resolve their current item's most recent permitted result and expand retrieval with the item context. Optional semantic retrieval runs before the owner transaction, then selected versions and prior-output dependencies are rechecked inside it. Final memory selection retains project and agent boundaries and lexical/graph fallback.
- Captured high-confidence direct assertions stay source-backed rather than verified. Context includes acquisition and confirmation explicitly. Tentative, quoted, corrected or uncertain capture statements remain candidates, including when extraction omits a qualifier from the surrounding source line. No confirmation gate was added to ordinary explicit assertions.

## Regression checks

`internal/workspace/ownership_test.go`: mixed manual/generated edits, multi-source replacements, bounded large edits.

`internal/postgres/ux_regressions_test.go`: consecutive ask/edit/reuse, forged/cross-owner history, revocation preserving ordinary history, atomic delegation and retries, missing setup rollback, mixed revocation/deletion/promotion, current-item continuation, semantic provider call outside the owner lock, qualified memory, and legacy quarantine access.

`web/tests/hall-ux.spec.ts`: clear delegation starts once and survives reload, ambiguous discussion creates no unwanted work, and consecutive follow-ups submit server turn IDs. These use mocked API responses to exercise the built frontend; backend integration tests separately cover persistence/idempotency.

The PostgreSQL helper now rejects any database whose server encoding is not UTF8. Use PostgreSQL 16 and pgvector 0.8.2, as in CI. Migration 013 remains unchanged.

## Local verification environment

Windows host, Go 1.26.8, Node 22.12.0, installed Chrome. Full native backend execution fails in existing `internal/ai/siwc` Unix-only `syscall.O_NOFOLLOW`/`Flock` code. No installed WSL distribution, runnable Docker CLI or PostgreSQL server was available. Linux vet and integration-test binary compilation can run; full backend/database and existing API-backed browser suites still require the parent's Linux verification environment. This branch must not be merged or deployed until those checks pass.

Passed locally: frontend lint, TypeScript checks, production build; all three mocked Hall browser scenarios; Go tests for workspace ownership, memory, blob, config and connectors; Linux-targeted vet for cmd/internal; Linux compilation of postgres and httpapi integration test binaries; Git diff whitespace validation. Compiled integration tests were not executed.
