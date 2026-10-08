# PCAS Project Status

Implementation snapshot for repository commit `c9ac3fd`. Recorded 2026-10-08.

This page separates product targets, code support, and recorded deployment evidence.
It does not report a new live check. Historical results apply to their stated data, channel, and date.
The [Whitepaper](whitepaper.md) defines the product. [Open Issues](tasks/backlog.md) records unresolved findings.

## Capability Status

| Capability | Repository status | Evidence and limits |
|---|---|---|
| Source storage, structured records, vectors, and versioned evidence | Implemented paths. | [Memory service](memory-service.md). A real-model recall baseline remains outstanding. |
| Memory groups, comparison, aliases, and current-state views | Implemented paths. | [Phase 2.6 rollout](evaluations/2026-10-07-phase2_6-rollout.md). Per-group status cards were removed. |
| Secretary actions and deputy work | Implemented paths. | [Desk processing](../internal/postgres/desk_turn.go) and [deputy processing](../internal/postgres/runs.go). Model quality still needs scenario checks. |
| Undo, reminders, and Telegram input | Implemented paths. | [Service reference](memory-service.md). Physical-device notification acceptance remains incomplete in the recorded backlog. |
| Project handovers, file area, document versions, effort, and start dates | Implemented paths. | [Phase 3 rollout](evaluations/2026-10-08-phase3-rollout.md). Calendar conflict detection is a future function. |
| Automatic tasks, ideas, and projects | Implemented paths. | [Phase 3.5 rollout](evaluations/2026-10-08-phase3_5-rollout.md). The quality of automatic topic projects needs review. |
| Date closure and background date review | Implemented paths. | [Phase 3.6 rollout](evaluations/2026-10-08-phase3_6-rollout.md). Background judgment and undo quality need continued observation. |
| Generic input and AI archives | Implemented paths. | [Input reference](connectors.md). Native email, calendar, Yufolo, and other IM adapters are not established by generic input support. |
| Model providers and manual selection | Implemented paths. | [Deployment](deployment.md). Automatic model selection is a target. |
| Observation panel | Partial controls. | Library and settings contain memory, work, and cost information. The complete panel is not delivered. |
| Habit statistics, household finance, and external correspondence | Product targets. | Some input and learning hooks exist. They do not establish these functions. |
| Training and personal models | Sample selection and export exist. | Training and model feedback into the active pool remain targets. |
| Native phone application and screen capture | Product targets. | Web/PWA and Telegram do not establish background GPS or microphone collection. |

## Recorded Delivery

| Phase | Recorded state | Record |
|---|---|---|
| Phase 1 | Secretary release on 2026-10-01. | [Acceptance](evaluations/2026-09-30-phase1-acceptance.md). |
| Stabilization | Fixes recorded. Some live and device checks remain open. | [Acceptance](evaluations/2026-10-01-stabilization-acceptance.md). |
| Phase 2 | Batches released on 2026-10-02 and 2026-10-03. | [Historical phase entry](tasks/phase2/README.md). |
| Phase 2.5 | Memory organization released on 2026-10-05. | [Acceptance](evaluations/2026-10-05-phase2_5-batch234-acceptance.md). |
| Phase 2.6 | Current-state revision released on 2026-10-07. | [Rollout](evaluations/2026-10-07-phase2_6-rollout.md). |
| Phase 3 | Workspace release on 2026-10-08. | [Rollout](evaluations/2026-10-08-phase3-rollout.md). |
| Phase 3.5 | Automatic-item release on 2026-10-08. | [Rollout](evaluations/2026-10-08-phase3_5-rollout.md). |
| Phase 3.6 | Date-review release on 2026-10-08. | [Rollout](evaluations/2026-10-08-phase3_6-rollout.md). |

Phase 2.0 was rolled back on 2026-10-01.
Its design is stored under the recorded `archive/phase2-0/*` tags.
The earlier implementation is on the `legacy` branch. Neither is a current development specification.

## Known Boundaries

- The Chengdu example in the former whitepaper was an illustration, not a measured recall result.
- A fixed scenario report does not establish recall quality for all data or requests.
- Phase 3.5 scheduling failed with more eligible topics than the acceptance fixture supplied.
- Phase 3.6 check mode skips memory date changes. The recorded reply could still claim that the change succeeded.
- Automatic topic projects can treat a past topic as current work. The rollout records a questionable travel project.
- Document removal uses immediate undo. The wider deletion policy requires confirmation; issue 38 retains this discrepancy.
- The latest home layout is a design direction. This document does not claim a separate deployment for that layout.
- The direct ChatGPT channel has a recorded login and generation check, but no complete real-account lifecycle acceptance.

## Documentation Changes

Product requirements are merged into the whitepaper.
ChatGPT account setup is merged into deployment instructions.
Lasting development rules are consolidated in the workflow.
Old stage instructions and research are marked as historical material.
Their bodies remain available for evidence and contract history.
Completed backlog rows are in [Resolved Issue History](history/resolved-issues.md).
The [Writing Guide](writing-guide.md) defines English terms and STE100 review rules.
