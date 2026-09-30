# Memory and permission boundary repair — 2026-09-30

The review originally inspected `d4c2178`; implementation was started on
`9bb7662` and rebased onto `28388e2` to preserve the new answer-card follow-ups
and home-city setting. No production data or model credentials were used.

## Changes

- Derived notes, body, progress and generated task titles retain provenance
  through edits and idea-to-task promotion. Existing promoted items are repaired
  before use/deletion. Permissions and revisions are checked before and after
  desk generation.
- Client-held generated answers are not replayed into later model requests.
  Follow-up user questions and current permitted retrieval remain available.
  Delegating a conversation copies user questions, not untracked generated text.
- High-confidence, verbatim direct current captures can be source-backed
  (`sourced`), distinct from user confirmation. Imported, historical, quoted,
  ambiguous and model-authored material still requires review. Existing inferred
  records are not mass-promoted.
- Replacing a source version suppresses its obsolete automatically extracted
  claims in current views, invalidates dependent output and rejects late worker
  completions. Historical evidence and explicit user confirmations remain intact.
  Exact repeated assertions attach the new evidence to the same claim.
- Queued assistant runs use the shared scoped lexical/graph retrieval path
  rather than filling context solely by global recency. Current preferences and
  decisions remain eligible as standing context. This transactional path does
  not make an embedding-provider call while holding the owner lock; the Recall
  endpoint continues to use optional embeddings when configured.
- Recall ranks actual matching chunks and returns their text and offsets. Before
  chunking completes, truncation centers on a matching passage instead of always
  returning the document prefix. Graph expansion uses the same knowledge and
  effective-time constraints as the initial recall.
- Failed routing asks the user to choose. Delegation creates an item, preserving
  the question, but model execution happens explicitly from the item page.

## Regression coverage

New tests cover edited and promoted derived content, pre-fix promotion repair,
revocation/deletion and mid-generation revocation; direct capture into default
assistant context; source replacement, repeated assertions, historical knowledge
and stale extraction; relevant old facts amid recent noise; tail-only evidence
before/after chunking; and graph effective/knowledge-time boundaries.

`web/tests/desk-routing.spec.ts` covers routing-provider failure, cancelling back
to the input, retrying, creating a task and ensuring no automatic run request.
The dedicated PR workflow runs it against an isolated PostgreSQL/pgvector-backed
instance with no model account.

## Deliberate limits and review points

- Provenance is currently field-level, not block-level. If manual and derived
  text share one field, revocation withholds that field together; source deletion
  clears the affected field. This favors confidentiality over retaining mixed
  text. A future block editor can narrow this without exact-string matching.
- Arbitrary copies or rewording across independent sources are not automatically
  merged. Cross-source entity resolution and contradiction reconciliation need
  a separate policy; equal names and last-import-wins are not safe substitutes.
- Query-only follow-ups can be less fluent without previous generated answers.
  Safe replay requires server-persisted answers with complete provenance and
  current access checks; a client-provided list of citations is not sufficient.
- No production deployment, authenticated live-model acceptance, broad semantic
  recall benchmark or claim of complete security audit is included.
- Local validation uses Go 1.26.8, PostgreSQL 17 and pgvector 0.8.0. Repository CI
  uses PostgreSQL 16/pgvector 0.8.2. The cloud's local Chromium launch is blocked
  by unavailable Unix sockets; browser regression must be verified in CI rather
  than represented as locally passed.
