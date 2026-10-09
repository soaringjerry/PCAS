# PCAS Project Status

Capability snapshot base: repository commit `c9ac3fd`, recorded 2026-10-08.
Foundation delivery records updated 2026-10-09.

This page gives product targets, implemented functions, and recorded deployment evidence with different status labels.
Each delivery record identifies its checks. Historical results apply to their stated data, channel, and date.
The [Whitepaper](whitepaper.md) gives product requirements.
The [Roadmap](whitepaper.md#10-roadmap) gives the phase sequence, delivery scopes, and acceptance targets. [Open Issues](tasks/backlog.md) records unresolved findings.

## Capability Status

| Capability | Repository status | Evidence and limits |
|---|---|---|
| Source storage, structured records, vectors, and versioned evidence | Implemented paths. | [Memory service](memory-service.md). A real-model recall baseline stays outstanding. |
| Memory groups, comparison, aliases, and current-state views | Implemented paths. | [Phase 2.6 rollout](evaluations/2026-10-07-phase2_6-rollout.md). Per-group status cards were removed. |
| Secretary actions and deputy work | Implemented paths. | [Desk processing](../internal/postgres/desk_turn.go) and [deputy processing](../internal/postgres/runs.go). Model quality must have scenario checks. |
| Undo, reminders, and Telegram input | Implemented paths. | [Service reference](memory-service.md). Physical-device notification acceptance stays incomplete in the recorded backlog. |
| Project handovers, file area, document versions, effort, and start dates | Implemented paths. | [Phase 3 rollout](evaluations/2026-10-08-phase3-rollout.md). Calendar conflict detection is a future function. |
| Automatic tasks, ideas, and projects | Implemented paths. | [Phase 3.5 rollout](evaluations/2026-10-08-phase3_5-rollout.md). The quality of automatic topic projects is under review. |
| Date closure and background date review | Implemented paths. | [Phase 3.6 rollout](evaluations/2026-10-08-phase3_6-rollout.md). Continue to observe background judgment and undo quality. |
| Generic input and AI archives | Implemented paths. | [Input reference](connectors.md). Generic input paths do not include native email, calendar, Yufolo, or other IM adapters. |
| Model providers and manual selection | Implemented paths. | [Deployment](deployment.md). Automatic model selection is a target. |
| Observation panel | Partial controls. | Library and settings contain memory, work, and cost information. The complete panel is not delivered. |
| Habit statistics, household finance, and external correspondence | Product targets. | Some input and learning hooks exist. These functions are targets. |
| Training and personal models | Sample selection and export exist. | Training and model feedback into the active pool stay targets. |
| Native phone application and screen capture | Product targets. | Web/PWA and Telegram do not include background GPS or microphone collection. |

## Recorded Delivery

| Phase | Recorded state | Record |
|---|---|---|
| Phase 1 | Secretary release on 2026-10-01. | [Acceptance](evaluations/2026-09-30-phase1-acceptance.md). |
| Stabilization | Fixes recorded. Some live and device checks stay open. | [Acceptance](evaluations/2026-10-01-stabilization-acceptance.md). |
| Phase 2 | Batches released on 2026-10-02 and 2026-10-03. | [Historical phase entry](tasks/phase2/README.md). |
| Phase 2.5 | Memory organization released on 2026-10-05. | [Acceptance](evaluations/2026-10-05-phase2_5-batch234-acceptance.md). |
| Phase 2.6 | Current-state revision released on 2026-10-07. | [Rollout](evaluations/2026-10-07-phase2_6-rollout.md). |
| Phase 3 | Workspace release on 2026-10-08. | [Rollout](evaluations/2026-10-08-phase3-rollout.md). |
| Phase 3.5 | Automatic-item release on 2026-10-08. | [Rollout](evaluations/2026-10-08-phase3_5-rollout.md). |
| Phase 3.6 | Date-review release on 2026-10-08. | [Rollout](evaluations/2026-10-08-phase3_6-rollout.md). |
| Phase 3.9 shared skeleton | First background scope released at `935ad5b`. Phase 3.9 remains incomplete. | [Acceptance and live check](evaluations/2026-10-08-foundation-shared-skeleton.md). |
| Phase 3.9 recorded replay | Diagnostics and approved business-clock extension released at `b39a9ea` on 2026-10-09. Complete state equivalence remains incomplete. | [Replay and release evidence](evaluations/2026-10-08-foundation-recorded-replay.md#release-verification). |
| Phase 3.9 interactive gateway | Secretary, readers, and self-checks released at `022a912` on 2026-10-09. Phase 3.9 remains incomplete. | [Complete CI, replay, real Codex, and live checks](tasks/foundation-interactive-gateway.md#final-acceptance). |

Phase 2.0 was rolled back on 2026-10-01.
Its design is stored under the recorded `archive/phase2-0/*` tags.
The earlier implementation is on the `legacy` branch. These histories are not current development specifications.

## Known Boundaries

- The Chengdu example in the former whitepaper was an illustration, not a measured recall result.
- A fixed scenario report measures recall quality for its recorded data and requests only.
- Phase 3.5 scheduling failed with more eligible topics than the acceptance fixture supplied.
- Phase 3.6 check mode skips memory date changes. The recorded reply could still claim that the change succeeded.
- Automatic topic projects can treat a past topic as current work. The rollout records a questionable travel project.
- Document removal uses immediate undo. The wider deletion policy calls for confirmation; issue 38 records this discrepancy.
- The latest home layout is a design direction. This document has no deployment evidence for that layout.
- The direct ChatGPT channel has a recorded login and generation check, but no complete real-account lifecycle acceptance.

## Current Architecture Direction

The agreed next phase is [Phase 3.9 Foundation](whitepaper.md#phase-39-foundation), before further business repairs and Phase 4.
The [System Architecture](architecture.md) defines its scope and completion criteria.
The foundation is not complete or accepted.
The [shared skeleton](tasks/foundation-shared-skeleton.md) adds the first background gateway, prompt registry, and call metadata path.
Its [acceptance record](evaluations/2026-10-08-foundation-shared-skeleton.md) separates checks from deployment and remaining work.

The user reports incorrect projects, timelines, and tasks, with increasing model use, cost, and duration.
These reports need attributed cases and measurements. They do not establish a measured trend or a single cause.
Keep the cases for diagnosis through the new architecture.

The architecture's code findings use revision `6b62f27`.
They do not update this page's implementation snapshot or supply new deployment evidence.

The [foundation inventory](evaluations/2026-10-08-foundation-inventory.md) records production paths and an existing-record baseline at code revision `4af34cd`.
It includes exclusions before model calls and missing expected actions.
At that inventory revision, no architecture code had changed.
Controlled scenario baselines remain necessary before the corresponding production paths change.

The [event contract](architecture.md#event-records-and-triggers) uses the existing queue for declared specialist processing and causal records.
Central event responses and chain limits remain targets.
The shared skeleton does not implement them.

The [five-layer test contract](architecture.md#five-test-layers) is required for Phase 3.9 acceptance.
Architecture checks come first. Recorded production-copy replay comes second, before further business-domain migration.
Owner contracts, fixed concurrency scenarios, and nightly real-model score trends remain required work.
These requirements are not a claim that the five layers are complete.

### Foundation Delivery Milestones

These milestones expand the [roadmap](whitepaper.md#phase-39-foundation) into delivery order.
They have different workloads. Their count does not establish a completion percentage.
The [architecture acceptance](architecture.md#10-foundation-acceptance) governs phase closure.

| Order | Delivery | Current state |
|---|---|---|
| First | Architecture documents, responsibilities, and existing-path inventory. | Initial direction and inventory recorded. Refresh affected paths before each migration. |
| Second | Shared gateway, registered prompts, and architecture checks. | Background skeleton released at `935ad5b`; interactive calls released at `022a912`. Complete provider and owner coverage remains outstanding. |
| Third | Recorded production-copy replay. | [Diagnostics and clock extension released](evaluations/2026-10-08-foundation-recorded-replay.md#release-verification) at `b39a9ea`. Background and secretary transport replay work without live calls. Complete state equivalence remains incomplete. |
| Fourth | Owner pilot, then complete business paths and remaining model calls. | Domain extraction has not started. Keep temporary adapters until their named callers migrate. |
| Fifth | Unified activity queries, declared event responses, and causal-chain limits. | Targets. Existing records and queue supply their starting mechanisms. |
| Sixth | Complete five-layer and whole-system acceptance. | Outstanding. Include resource comparisons and traces for incorrect and missing expected actions. |

The order identifies dependencies. Compatible scopes can proceed together after their prerequisites are established.
Start nightly evaluation after complete gateway coverage under the architecture's [test contract](architecture.md#five-test-layers).

The coordinator approved the [interactive gateway scope](tasks/foundation-interactive-gateway.md) before owner extraction.
Its [final acceptance](tasks/foundation-interactive-gateway.md#final-acceptance) records release at `022a912`.
Secretary generation, heavy readers, secretary self-check, and deputy self-check now use the existing gateway.

The [deputy batch](tasks/foundation-deputy-gateway.md) has reservation and durable-input preparation through CI-tested revision `892b2b3`.
Its fixed-clock ordinary and revision recordings pass strict offline transport replay.
Complete owner data differences remain recorded.

Main generation and revision now use the gateway in the deputy branch.
Saved-result recovery and exact application links are implemented there. Filtered repository and integration checks pass.
The ordinary recording rejects replay after a deadline crossed its actual-time boundary.
Final replay and complete CI remain outstanding.
This batch has not been released and does not complete Phase 3.9.

Registered instructions and four output schemas preserve their original bytes and field order.
The secretary keeps its original shared deadline and two-attempt retry policy.

Invocations link their inputs, results, reservations, usage, and application outcomes.
Unknown outcomes remain visible and retain their reservations. Accounting recovery uses the existing worker without another provider call.
Each reader page has its own stage identity. Failed checks preserve accepted draft data and show their incomplete state.

Complete integration-branch and main CI passed. Strict secretary and extraction replay started no live provider.
The comparisons retained all owner columns. Remaining timing, capture timestamp, and legacy identity findings prevent a complete equivalence claim.

Real default Codex passed on the isolated copy and after release. Backup, live claim counts, and own-request cleanup passed.
The additional local serial check was interrupted in an existing snapshot query; its cause remains unconfirmed.

Full deputy generation, audio, vision, embeddings, routing, and legacy entry points still need complete migration or retirement.
Domain extraction, unified activity, event responses, and the remaining test layers stay outstanding.

## Documentation Changes

Product requirements are merged into the whitepaper.
ChatGPT account setup is merged into deployment instructions.
Lasting development rules are consolidated in the workflow.
Previous stage instructions and research are marked as historical material.
Their bodies stay available for evidence and contract history.

Completed backlog rows are in [Resolved Issue History](history/resolved-issues.md).
The [Writing Guide](writing-guide.md) gives English terms and STE100 review rules.
