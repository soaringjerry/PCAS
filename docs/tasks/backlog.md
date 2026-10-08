# PCAS Open Issues

Recorded findings, reviewed for document organization on 2026-10-08.

This list does not claim a fresh reproduction of each defect.
An assigned task, passing CI, or implemented hook does not establish closure.
Check the current code path and recorded result before assigning or closing an item.
[Resolved Issue History](../history/resolved-issues.md) retains the original completed rows.
[Project Status](../status.md) separates targets, code, and delivery evidence.

## Recorded Open Findings

| ID | Finding | Required evidence or decision | Source |
|---|---|---|---|
| 1 | Physical-device notifications lack complete acceptance. | Check phone display and actual Telegram delivery. | Phase 1 acceptance. |
| 2 | Deputy failures use overly general messages. | Check stable error categories and recovery guidance. | Phase 1 acceptance. |
| 3 | Undo of adoption does not reverse memory reinforcement. | Decide and verify the activity effect of undo. | Phase 1 review. |
| 4 | Adoption records remain after undo. | Check cleanup and any protection effect. | Phase 1 review. |
| 6, 7 | Unused Jev routing and the old answer API remain. | Establish remaining callers and removal conditions. | [Service reference](../memory-service.md). |
| 8 | The one-sentence card reply relies on the prompt. | Observe real replies before adding a program limit. | Secretary acceptance. |
| 9, 31 | Internal controls remain in library and settings. | Move applicable controls when the observation panel is delivered. | [Interface principles](../design/principles.md). |
| 10 | Action receipt sources enter indexing and summaries. | Check retrieval interference and actual processing use. | Postlaunch review. |
| 14 | Old content from connected inputs can lack historical classification. | Check the connector-to-extraction path before enabling another native input. | Archive-import review. |
| 15 | Imported relative plans can lose their original statement time. | Verify statement time and event time separately. | Archive-import review. |
| 17 | Historical stabilization live checks are incomplete in the record. | Confirm remaining scope. The old one-day trial is not a new development prerequisite. | [Stabilization entry](stabilization/README.md). |
| 20 | Source titles can expose generated internal labels. | Check titles through the actual source-view path. | Phase 2 source-view acceptance. |
| 25 | A cancellation test has an intermittent timeout. | Distinguish a timing-sensitive test from a product race. | Phase 2 CI record. |
| 26 | The activity selector lists only recent memories. | Check search and pagination when moving it to the observation panel. | Phase 2 interface acceptance. |
| 27 | Bulk extraction concurrency limits throughput. | Measure call capacity, rate limits, and secretary interference before changing concurrency. | Import review. |
| 28 | The old task requested conversation-level extraction. | Phase 2 batch 4b reports delivery. Verify the disposition before closing this stale row. | [Batch 4b acceptance](../evaluations/2026-10-03-phase2-batch4b-acceptance.md). |
| 32, 62 | Old migration fixtures impose compatibility branches on current production writes. | Check isolated migration coverage and remove unsupported fixture dependencies. | [Migration tests](../../internal/postgres/phase2_b1_migration_test.go). |
| 33 | The real-model recall evaluation and baseline remain unrecorded. | Run the defined comparison on the actual channel. | Phase 2 evaluation task. |
| 34 | The direct account lacks full lifecycle acceptance. | Verify generation, refresh, generation after refresh, and revocation. | [Deployment](../deployment.md#4-chatgpt-direct-channel). |
| 35 | The home schedule lacks the complete date-block presentation. | Check the current layout against the interface specification. | Product review. |
| 36 | Imported conversation media is not fully parsed. | Define and verify selective attachment parsing. | Archive input decision. |
| 37 | Deferred bulk imports can slow job selection. | Measure queue selection with many deferred records. | Import queue review. |
| 38 | Document removal uses undo while the general deletion policy requires confirmation. | Resolve the scope of the recorded document-removal exception. | [Phase 3 decision](phase3/README.md). |
| 39 | Recall scoring can scan large record sets. | Measure current retrieval at representative scale before changing its ranking. | [Retrieval path](../../internal/postgres/retrieval.go). |
| 40 | Web search is channel-dependent. | Establish which configured providers supply search and show capability gaps. | Search task. |
| 41, 48 | Evaluation coverage and human review are insufficient in some categories. | Review current datasets and discriminatory value. Do not reuse early experiment claims as a baseline. | [Memory evaluation](../evaluations/2026-10-04-phase2_5-v2-doing.md). |
| 43 | Public and team retrieval use different lexical/vector ranking paths. | Check rank fusion and fallback behavior before making a unified ranking claim. | [Service reference](../memory-service.md). |
| 44 | A historical import still had unprocessed messages. | Check current progress by import/source identity. The old count is not a current count. | Import progress record. |
| 45 | Common conversation-branch content can be extracted more than once. | Verify shared-message identity and duplicate evidence handling. | Import evaluation. |
| 47 | Bulk extraction can delay memory organization. | Verify progress for new input and existing background work under sustained import. | Organization review. |
| 49 | Concurrent secretary and background model use had failures. | Check current retry behavior and real-channel failure evidence before treating the old report as current. | [Q5 task](improvements/Q5-secretary-model-error-under-background-load.md). |
| 51 | A first request timed out during startup. | Reproduce and inspect the provider process if this recurs. | Startup rollout record. |
| 52 | Snapshot organization statistics were slow. | Measure the current query and dataset. | Turn-speed review. |
| 56 | The classification distribution was dominated by opinions. | Review labeled samples with the current rule version. | Memory audit. |
| 57 | Activity signals need enough real use to affect ranking. | Compare ranking after sufficient observed use. | Memory audit. |
| 58 | Group filters require multiple page reads. | Verify server-side search and combined filters. | Phase 2.6 interface review. |
| 59 | Background deferral appears as scheduling failure. | Separate deferral and error in observation data. | Phase 2.6 acceptance. |
| 63 | Reusable methods and completion judging are deferred targets. | Discuss scope after workspace behavior is reliable. | Phase 3 scope. |
| 64 | Outside changes can prevent check-group cleanup. | Define safe handling without overwriting user work. | [Phase 3.5 rollout](../evaluations/2026-10-08-phase3_5-rollout.md). |
| 65 | Old candidates have no recorded reason. | Preserve the missing reason. Do not invent a historical justification. | Phase 3.5 rollout. |

## Latest Recorded Follow-up

The [Phase 3.6 rollout](../evaluations/2026-10-08-phase3_6-rollout.md) retains these follow-ups:

- Check completed background date-review decisions and user restorations.
- Review automatic topic projects for past events mistaken as current work.
- Correct the check-mode reply when the memory action is skipped.
- Verify the date-review activity row through the actual live interface.

These findings are separate from the future requests to change date times, review all historic intentions, and extend the observation panel.
