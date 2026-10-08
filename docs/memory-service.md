# PCAS Service Reference

Implementation reference for repository commit `c9ac3fd`. Updated 2026-10-08.

The Go service stores memory and workspace state in PostgreSQL.
The browser stores drafts and interface preferences, not the authoritative fact database.
The [Memory Architecture](memory-architecture.md) defines required behavior.
[Project Status](status.md) records delivery and validation limits.

## 1 Code Boundaries

| Concern | Code |
|---|---|
| Memory interfaces and record types | [Memory package](../internal/memory/contracts.go). |
| Workspace types and commands | [Workspace model](../internal/workspace/model.go). |
| HTTP authentication and memory routes | [HTTP server](../internal/httpapi/server.go). |
| Source storage and migrations | [Database](../internal/postgres/database.go) and [migrations](../internal/postgres/migrations/). |
| Retrieval | [Retrieval](../internal/postgres/retrieval.go) and [query hints](../internal/memory/query_plan.go). |
| Secretary preparation, model work, and execution | [Desk turn](../internal/postgres/desk_turn.go) and [actions](../internal/postgres/desk_actions.go). |
| Commands and undo | [Commands](../internal/postgres/commands.go) and [action log](../internal/postgres/actions_log.go). |
| Background queue coordination | [Worker](../internal/worker/worker.go). |
| Date closure and review | [Date completion](../internal/postgres/deadline_completion.go) and [date review](../internal/postgres/date_tidy.go). |
| Project creation and planning | [Topic projects](../internal/postgres/topic_projects.go) and [effort](../internal/postgres/effort.go). |

Many business operations remain in the PostgreSQL package.
The package name does not establish a separate application-service boundary.
The public retrieval path and team retrieval path have different inputs and ranking options.
Query hints currently use deterministic text rules. Their existence does not establish complete natural-language interpretation.

## 2 HTTP Interfaces

Authentication uses an owner session or an applicable Bearer token.
The service binds the owner scope. External principals use explicit grants.
Connector tokens can write only to their connector.

| Interface | Purpose |
|---|---|
| `POST /v1/session`, `DELETE /v1/session` | Start or end an owner session. |
| `GET /v1/workspace` | Read workspace state, revisions, processing status, and budget. |
| `POST /v1/workspace/commands` | Execute a command with `requestId` and applicable revision checks. |
| `POST /v1/desk/turn`, `GET /v1/desk/turns` | Execute a secretary turn or read conversation history. |
| `DELETE /v1/desk/smoke/{id}` | Clean an identified check-only group. |
| `GET /v1/workspace/schedule`, `/in-progress` | Read timed work or untimed task progress. |
| `GET /v1/workspace/handover`, `/deadlines`, `/assistant-requirements` | Read current-state information. |
| `GET /v1/workspace/memory-groups`, `/memory-groups/{key}/memories` | Read memory groups and paginated members. |
| `GET /v1/workspace/memories`, `/memories/{id}`, `/memory-facets` | Search memory, read one memory, or read filter values. |
| `GET /v1/workspace/projects/{id}/handover`, `/timeline` | Read project status or plan. |
| `GET /v1/workspace/documents/{id}/versions`, `/versions/{version}`, `/diff` | Read document history and differences. |
| `GET`, `POST /v1/workspace/items/{id}/files` | Read or add workspace files. |
| `DELETE /v1/workspace/items/{id}/files/{sourceId}` | Remove a workspace file. |
| `GET /v1/workspace/export` | Export records or selected training samples. |
| `POST /v1/memory/sources` | Store a versioned text source. |
| `GET /v1/memory/sources/{id}` | Read source content and processing state. |
| `GET /v1/memory/sources/{id}/conversation` | Read neighboring messages on the same branch. |
| `POST /v1/memory/attachments` | Store an attachment. |
| `GET /v1/memory/sources/{id}/attachment` | Download the authenticated original. |
| `POST /v1/memory/commit`, `/correct`, `/delete`, `/use` | Write structured data, correct data, delete data, or record use. |
| `POST /v1/memory/recall`, `/expand` | Retrieve references or expand their data and evidence. |
| `POST /v1/memory/summary`, `/activity` | Request a dependent summary or set activity parameters. |
| `GET /v1/memory/capabilities` | Read available capabilities and configuration gaps. |
| `GET`, `POST /v1/connectors` | Read or configure input sources. |
| `GET /v1/models` | Read model information without credentials. |
| `/v1/chatgpt/account`, `/login`, `/logout`, `/limits`, `/models` | Manage the Codex channel. |
| `/v1/chatgpt/direct/account`, `/login`, `/callback`, `/select`, `/logout` | Manage the optional direct channel. |
| `/v1/notify/config`, `/push-subscriptions`, `/telegram`, `/test`, `/notices/{id}/dismiss` | Manage notification delivery. |

Full route methods and optional interfaces are in the [HTTP package](../internal/httpapi/).
Command types are in [Frontend Actions](../web/src/store/actions.ts).
`expectedRevision` is not a universal lock for every command. Check the command's actual validation path.
Legacy `/v1/desk/answer` and `/v1/desk/route` remain in code; the current secretary does not use them.

## 3 Execution and Evidence

Secretary requests use request identities and conversation ordering.
Actions resolve server-supplied aliases. A model must not invent database identifiers.
The action path validates the result and records actual changes for undo.
Home date closure and secretary date closure share `completeDeadlineTx`.
That function marks a memory version. It does not delete the memory.

Source context preserves speaker, branch, statement time, and version.
The neighboring-message interface defaults to seven messages and permits up to twenty.
Model evidence expansion reads up to four source windows within 10,000 characters.
Each message excerpt is limited to 900 characters.
Clipped or unavailable context supplies a coverage gap.
These values are implemented in [Evidence Context](../internal/postgres/evidence_context.go).
The model must not treat an excerpt as the complete conversation.

The source write and work event share a transaction.
Workers commit with a lease and fencing token.
Derived results retain dependencies. Reads and result application check the applicable current versions and access.
Deletion follows scope and clears affected copies. Blocking markers contain no source text.
Training exports retain the selected samples and versions.

## 4 Current Processing Values

The values below describe code. They are not a new performance or model-quality guarantee.

| Background stage | Calls per rolling hour |
|---|---:|
| Organization | 40 |
| Statement comparison | 40 |
| Entity comparison | 30 |
| Entity candidate scan | 6 |
| Global handover | 2 |
| Project handover | 10 |
| Effort estimation | 10 |
| Topic project judgment | 2 |
| Date review | 30 |

[Background Budgets](../internal/postgres/background_budget.go) defines these limits.
Unused capacity is not transferred between stages. Excess work is deferred.
Do not copy an old phase's total as the current total.

Date review checks unresolved dates, recurring arrangements without a clock time, and dates more than one day past.
It records a decision per memory version and class.
It shows up to forty task titles and twenty idea titles to the model.
It clips source and memory text at 1,200 characters and counts clipping.
The prompt contains age-based guidance. Its decisions still need quality review.
See [Date Review](../internal/postgres/date_tidy.go).

Automatic topic projects use a minimum of ten current memories, a goal, and a date.
The model checks existing project meaning before creation.
The daily creation limit is four. Excess work can continue on a later day.
See [Topic Projects](../internal/postgres/topic_projects.go).

Start-date calculation uses four work hours per day and excludes weekends.
The user estimate is retained. See [Effort](../internal/postgres/effort.go).
These implementation values can change through an approved behavior change.

## 5 Validation Limits

CI includes unit tests, PostgreSQL integration tests, and browser checks.
Without `PCAS_TEST_DATABASE_URL`, PostgreSQL tests can skip; see [Test Setup](../internal/postgres/integration_test.go).
A skipped suite is not database validation.
Real account, device, performance, and recall checks have separate conditions.
The reports in the [History Catalog](history/README.md) state their own data and scope.
Passing mocked output checks does not prove prompt quality or correctness on large personal datasets.
