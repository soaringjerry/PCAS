# AGENTS.md

Rules for each executor who changes code in this repository.
An executor is a person or AI model that implements an assigned change.
Read this page before the task scope. Then read the task scope.

> 以暗猜接口为耻，以认真查阅为荣
> 以模糊执行为耻，以寻求确认为荣
> 以盲想业务为耻，以人类确认为荣
> 以创造接口为耻，以复用现有为荣
> 以跳过验证为耻，以主动测试为荣
> 以破坏架构为耻，以遵循规范为荣
> 以假装理解为耻，以诚实无知为荣
> 以盲目修改为耻，以谨慎重构为荣

The motto is a summary. The sections below give the rules.
Ask for confirmation only in the conditions of section 9.

## 1 Authority

The [Documentation Index](docs/README.md) gives the order of document authority.
This page adds code rules. It does not change product requirements.
If a task scope conflicts with this page, stop. Report the conflict to the coordinator.
Do not select one of the two rules yourself.

## 2 Required Reading

| Document | Read it for |
|---|---|
| [Whitepaper](docs/whitepaper.md) | Product requirements. |
| [System Architecture](docs/architecture.md) | Phase 3.9 scope, domain responsibilities, dependencies, and acceptance. |
| [Project Status](docs/status.md) | Implemented functions, targets, and gaps. |
| [Development Workflow](docs/tasks/process.md) | Checks, review, merge, and release. |
| [Writing Guide](docs/writing-guide.md) | Terms and English documentation rules. |
| [Service Reference](docs/memory-service.md) | HTTP interfaces and code paths. |

Historical task records do not assign current work.

## 3 Commands

```sh
make fmt
make check
PCAS_TEST_DATABASE_URL='postgres://...' make test-integration
cd web && npm run lint && npm run type-check && npm run build
```

Run `make fmt` and `make check` before each Git commit.
If you change `internal/postgres`, run `make test-integration`.
The test database must support pgvector.
If you change `web/`, run the three frontend commands.
If you do not run a check, write that in the PR. Give the reason.

## 4 Code Map

| Location | Contents |
|---|---|
| `cmd/pcas` | Service entry point. |
| `cmd/pcas-eval` | Evaluation and production-copy replay tools. Production does not use them. |
| `internal/postgres` | PostgreSQL adapter composed from 25 focused storage packages over `internal/postgres/kernel` (`callstore`, `budgetstore`, `itemstore`, `deputystore`, `deskstore`, `commandstore`, `workspacestore`, `intakestore`, `interpstore`, `statestore`, `usestore`, `activitystore`, and others), plus migrations, queue machinery, and schedulers. Storage makes no model calls and holds no prompt instructions. |
| `internal/postgres/migrations` | Database migrations. |
| `internal/memory`, `internal/workspace` | Memory and workspace types. |
| `internal/workspace/effort`, `internal/workspace/topicproject`, `internal/workspace/projecthandover`, `internal/workspace/secretary`, `internal/workspace/deputy`, `internal/workspace/itemcommand`, `internal/workspace/command`, `internal/workspace/settings`, `internal/workspace/reminder`, `internal/workspace/activity`, `internal/workspace/erasure`, `internal/workspace/access` | Workspace domain packages outside storage: effort estimation and scheduling, topic projects, project handovers, secretary turns and actions, deputy runs and adoption, single-item and workspace commands with undo, settings rules, reminders, activity wording, erasure rules, and run access rules. |
| `internal/currentstate/datereview`, `internal/currentstate/handover`, `internal/currentstate/schedule` | Current-state domain packages outside storage: remembered date review, global handover, and schedule view assembly. |
| `internal/interpretation/aliasscan`, `internal/interpretation/comparison`, `internal/interpretation/organize`, `internal/interpretation/extraction`, `internal/interpretation/claim` | Memory interpretation domain packages outside storage: alias scanning, statement and entity comparison, organizing and date schedule rules, extraction from sources and imported conversations, and statement normalization and deduplication. |
| `internal/input/attachment`, `internal/input/voice`, `internal/input/projectfile` | Input domain packages outside storage: attachment parsing, voice transcription, and project file rules. |
| `internal/contextbuilder`, `internal/contextbuilder/reader` | Serialization of memory and current state for agent model input, plus heavy memory readers shared by the secretary and the deputy. |
| `internal/retrieval` | Memory retrieval rules, recall orchestration, query and background vector indexing, and source excerpts outside storage. |
| `internal/app` | Application setup: gateway and service composition, stage limits, the central event-response table, and architecture boundary tests. |
| `internal/modelcall`, `internal/modelcall/jobcall`, `internal/modelcall/stagecall`, `internal/prompts` | Unified model gateway, background and stage call helpers, and embedded prompt and output-schema registry. |
| `internal/ai` | Model provider adapters. |
| `internal/httpapi` | HTTP interfaces. |
| `internal/telegram`, `internal/notify`, `internal/connectors` | Telegram channel, notification delivery, and input connectors. |
| `internal/worker` | Background job coordination. |
| `web/` | Frontend. |

Architecture boundary tests in `internal/app/boundaries_test.go` and `internal/modelcall/provider_boundary_test.go` enforce package dependencies, model-call boundaries, prompt ownership, item-table ownership, and call-journal ownership on every commit.

## 5 Before You Write Code

Before you add a function, type, constant, or SQL query, search the repository.
If a suitable one exists, use it.
If you do not use an existing one, give the reason in the PR.

Before you call a function, read its definition and its callers.
Do not use the name to guess its writes, errors, or side effects.

Before you use a database table, read its migration.
Do not use column names to guess constraints or meaning.

If the business meaning is not in a current document, ask the coordinator.
Do not invent the meaning.

## 6 Architecture Guardrails

The Phase 3.9 foundation structure in section 10 is in place.
Keep these boundaries intact. If a task needs an exception, stop and ask the coordinator.

1. Model call paths. Submit production model calls only through `internal/modelcall` (`Gateway`, `jobcall`, or `stagecall`).
   Do not call `internal/ai` providers directly from domains, channels, or storage.
2. Prompt forms. Store production instructions and output schemas in `internal/prompts` using `go:embed`.
   Do not keep prompt instructions in `internal/postgres` or assemble instruction prose inside workflows.
3. Activity, log, and usage tables. Use `model_calls`, `action_log`, `activity`, `model_usage`, `background_stage_events`, and `recorded_events` when possible.
   Do not create a new table for this purpose without an approved migration scope.
4. Business logic in `internal/postgres`. Put new business rules and workflow decisions in the responsible domain package outside storage.
   Keep SQL, locks, leases, and transactions in the corresponding storage package under `internal/postgres/`.

## 7 Tests

Name each test file and test function for the behavior that it checks.
Example: `undo_after_edit_test.go` and `TestUndoRestoresTitleAfterEdit`.
Do not use phase, batch, or task codes in new test names.
Examples of such codes: `phase2_5_b2_`, `p3_`, `r1_`, and `stabilization_`.

When you change an existing coded test file, move its tests to behavior-named files.
Do not rename files that your task does not change.

Test behavior, not implementation.
Do not assert the wording of prompt instructions.
Exception: output schemas keep their exact structure and field order. Workflow section 4 gives this rule.

If a refactor causes a test failure, identify the cause.
If behavior changed, repair the code or report the finding.
If only the implementation changed, you can delete the test. List each deleted test in the PR.
Do not change an expected value only to make a test pass.

Do not keep a copy of an earlier implementation in test code.
A task scope can request a temporary copy. Then write its removal condition at the top of the file.

## 8 Pull Requests

Write Git commit messages in English.
Start each message with `feat:`, `fix:`, `docs:`, `refactor:`, or `test:`.
Describe the behavior that changed, not the function name.
Example: `fix: a check-only turn skips close_date instead of failing the whole turn`.

Make one change in each PR.
Workflow section 5 gives the PR contents. Use these headings:

```
## Behavior
The final behavior and the implementation effect.

## Checks run
Each check. Real-model or data-copy checks and their observed results.

## Checks not run
Each check that you did not run, and the reason.

## Uncertain
Each judgment, business meaning, or affected function that you are not sure about.
Write "None" if there is nothing.
```

If you are not sure about a statement, write that you are not sure.
Do not write it as a conclusion.

If you change behavior that the user can see, update the current document in the same PR.

## 9 Stop and Ask

Stop and ask the coordinator in these conditions:

- The task scope conflicts with this page or a current specification.
- The task needs an exception to section 6.
- The task changes model input or an output schema. Workflow section 3 requires a real-model check first.
- The task needs a database migration.
- The task deletes or overwrites data, and undo cannot restore it.
- Two repairs of the same defect failed. Collect the real prompt and raw output before a third attempt.

In other conditions, complete the task scope. Do not stop for routine choices.

## 10 Foundation Structure and Follow-up

Phase 3.9 built the foundation structure below. Remaining Phase 4 and follow-up items stay tracked in [Project Status](docs/status.md) and [System Architecture](docs/architecture.md).

| Part | Delivered state |
|---|---|
| `internal/prompts` | Production prompts and output schemas live in `internal/prompts` with `go:embed` and content hashes. Storage packages cannot import the prompt registry. |
| Model gateway | All production generation, embedding, vision, and transcription calls enter `internal/modelcall`. `internal/postgres/callstore` provides one unified `Journal` for background and interactive calls plus `DirectCalls` for direct calls. |
| Activity stream | The owner-scoped day activity query (`internal/postgres/activitystore`) answers one local day's calls, actions, reversals, runs, stage outcomes, decisions, costs, and explicit link gaps. Visual presentation and controls stay for Phase 4. |
| Domain and storage split | Business rules and orchestration live in domain packages outside `internal/postgres`. Storage is split into 25 focused packages over `internal/postgres/kernel`, with `itemstore` owning `work_items` writes and `callstore` owning `model_calls` and paid results. |
| Events and evaluation | `internal/app/responses.go` is the single event-response table, queue jobs carry causal identities in `recorded_events` with a 200-depth chain limit, and nightly real-model evaluation runs on the Codex channel. Committed-change events beyond `work.queued` and per-event response limits remain follow-up work. |

The [System Architecture](docs/architecture.md) gives the boundaries and acceptance criteria.
