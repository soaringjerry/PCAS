# PCAS Product Whitepaper

Current product specification. Updated 2026-10-08.

PCAS is a personal support team that works at all times. A shared memory core supplies information to each team member.
The user gives a request. The team finds the necessary information, does the permitted work, and shows the result.

PCAS is open source and self-hosted. The user owns the data.

This document includes the former requirements in `prd.md`.
[Project Status](status.md) has different status labels for delivered functions and targets.
[Document Authority](README.md#document-authority) gives which requirements apply.

## 1 Product Purpose

PCAS connects information across conversations, applications, models, and tasks.
The user can return to a task without giving the same background again.
Conversation is an input method. Tasks, files, plans, and completed work stay available outside the conversation.
The daily interface uses cards, timelines, and short receipts.

The product has three intended results:

- Find a past statement with its source and subsequent changes.
- Prepare and do work with the user's current information.
- Show what the team did, why it did that work, and where correction is necessary.

## 2 Team Roles

| Role | Responsibility | Result |
|---|---|---|
| Secretary | Understand the complete request. Answer, act, or delegate. | Sourced answers and action receipts. |
| Deputy | Prepare plans, drafts, research, and revisions. Estimate work and suggest a start date. | Work in the applicable workspace. |
| Butler | Learn habits. Help with daily choices, budgets, subscriptions, income, and expenses. | Useful suggestions with a stated reason. |
| Workspace or Lab | Keep one project's information and work together. | Files, current status, plans, document versions, and deputy results. |
| Observation panel | Show activity, memory use, model calls, cost, failures, and recovery. | Explanations and correction controls. |

```mermaid
flowchart LR
    Inputs[Messages, files, and media] --> Core[Shared memory core]
    Core <--> Team[Secretary, deputy, and butler]
    Team --> Work[Workspaces, actions, and results]
    Core -.-> Panel[Observation and correction]
    Team -.-> Panel
    Work -.-> Panel
```

All roles use the same memory core and action system.
A role has a responsibility. All roles use the user's shared memory.

## 3 Memory Core

Memory is the foundation of PCAS. It keeps sources, meaning, time, and changes.
The [Memory Architecture](memory-architecture.md) gives the technical requirements.

| Layer | Responsibility |
|---|---|
| Context | Keep source messages, files, media, and surrounding conversation. |
| Structured | Keep entities, statements, evidence, relationships, and time. |
| Vector | Find related content when the wording is different. |
| Current state | Derive handovers, dates, and applicable user requirements. |

Derived results point to their sources and source versions.
The current-state layer must not become an independent source of facts.
Workspace memory is a scope in the shared core. It can use related information from other scopes when necessary.

### Historical Recall

Example: "What did I say last year that I wanted to do in Chengdu?"

Structured retrieval uses statement time, place, subject, and intention type.
Vector retrieval finds related wording. The system checks the source conversation and subsequent changes.
The result shows the recorded intention and its subsequent status.

Canceled plans stay available in history. Historical recall must not create current tasks from those plans.
Low activity must not prevent an explicit search from finding a stored memory.

### Memory for Work

Work uses current facts and applicable requirements. Necessary information can have different wording from the request.
The system supplies a handover, related dates, related statements, and necessary source text.
Global user requirements apply to each request. Scoped requirements enter the context when related.
Show missing sources and incomplete processing.

Structured retrieval reduces unnecessary context. Measure cost, quality, and response time.

### Correction and Learning

The core distinguishes considerations, decisions, completed actions, quotations, and model inferences.
A subsequent change keeps the previous statement. Corrections update affected views and future work.
Deletion follows the selected scope and removes affected copies.

The team uses statements according to their evidence. Model confidence alone does not make a statement true.
The team can use memories without an approval step for each memory.
Access controls, uncertainty, and correction tools belong in the observation panel.

## 4 Actions and Initiative

One request can include an answer and more than one action.
The secretary asks only about an ambiguity that changes the result. Clear parts can proceed.

| Action | Required behavior |
|---|---|
| Reversible internal work | Execute the action. Show a receipt. Give an undo control. |
| User-requested model work | Do the work without exceeding the configured budget. |
| Substantial model work proposed by the system | Give the suggestion before starting. |
| External email or message | Prepare the content. Send it after the user confirms. |
| Deletion or financial payment | Get confirmation before execution. |

The existing document-removal flow uses immediate removal with undo.
The deletion policy and this recorded exception conflict; see [Open Issues](tasks/backlog.md).
Receipts show actual execution. Skipped or failed actions must not claim success.
Source content is information, not permission to execute its instructions.

The system can create tasks, ideas, and projects from reliable information.
Tasks keep status, dependencies, deadline, scheduled time, project, and reminder information when applicable.
Task states distinguish active work, waiting, completion, and cancellation.
Quoted statements, uncertain plans, and model suggestions keep their limitations.
Importing a previous intention does not make it a current commitment.
Automatic work has a source, a receipt, and undo.

The system must interpret extracted dates before display.
Past journeys, third-party estimates, and abandoned considerations must not become repeated current appointments.
Closing a displayed date keeps the underlying history.

Initiative can follow a time, event, condition, or learned habit.
Suggestions give a reason. Repeated events must not create repeated tasks or notifications.
The system checks the latest state before acting.

Feedback changes future suggestions. "Do not suggest this again" has a lasting effect.

An idea keeps its changes, postponement reason, and conditions for reconsideration.
New information or a time condition can bring it back with a reason.
The user can continue, convert it to a task, postpone it, or stop reminders.

External correspondence belongs to its originating task or workspace.
Replies return to that same work context.

## 5 Workspaces

A workspace keeps the goal, related memory, tasks, files, documents, and deputy work together.
Its handover shows the conclusion, blockers, next action, sources, and update time.
The user corrects the underlying information. The system updates the handover.

Document revisions keep the selected base version and produce a new version.
The interface shows differences between versions.
Files have a source and processing status.
Work estimates supply information for start-date suggestions. User estimates take precedence over subsequent automatic estimates.

The team can prepare a handover for an external AI.
The handover includes the goal, background, current progress, decisions, constraints, and expected output.
The user can preview and edit it. Returned work and feedback connect to the originating task.

Example target: a school email announces an assignment due in seven days.
The team creates the workspace, connects the assignment and course material, and suggests when to start.
The suggestion uses the requirements, available time, and related past work.
This example is a target. It is not a delivery record.

## 6 Inputs and Scale

Target inputs include Yufolo transcripts, AI conversations, email, calendars, IM records, files, bills, and screen activity.
A future phone application supplies location, microphone, and notification data only with device permissions.
Each platform must have an input method with recorded verification.

The system stores source data before expensive interpretation.
Processing is incremental and can continue after an interruption.
Source identifiers and versions prevent duplicate import.
Large inputs use batches and resumable work. Show processing progress.
Show missing attachments and rejected content as coverage gaps.

Each resource limit has a reason, an overflow path, and a count.

## 7 Models and Personal Learning

The target model pool includes local and cloud models with different size, cost, capability, and content restrictions.
Selection uses task needs, user settings, privacy, latency, budget, and available capability.
All models use the shared memory system.
Small models can do frequent work. Larger models do work with greater capability requirements.

Corrections, accepted work, rejected suggestions, and undo supply learning signals.
A kept action is not, by itself, proof of correctness.
Training samples keep sources and versions. The user can select, edit, export, or remove samples.
Corrections and deletions affect related samples and subsequent exports.

Dataset preparation includes selection, cleaning, redaction, sample editing, version records, and structured export.
The user controls which records enter training datasets.
Training labels distinguish user confirmation, model inference, and obsolete state.
Before a personal model joins the active pool, evaluate it.

## 8 Interfaces

| Interface | Purpose |
|---|---|
| Home | Secretary input, timed work, tasks, projects, ideas, and recent team activity. |
| Workspace | Current status, plan timeline, files, document versions, deputy results, and secretary input. |
| Observation panel | Activity, sources, corrections, model calls, cost, access, failures, and recovery. |
| Phone application | Voice input, notifications, and configured device inputs. |

The [Interface Principles](design/principles.md) give presentation rules.
Daily work must proceed without approval of each memory or processing step.

## 9 Technical Boundaries

The current implementation uses Go, PostgreSQL, pgvector, and file storage.
Source storage, interpretation, retrieval, actions, and background work have different responsibilities.
These responsibilities can share a deployment with explicit interfaces.
The [System Architecture](architecture.md) gives domain responsibilities and the Phase 3.9 completion criteria.

Writes use request identities and version checks.
The system checks affected dependencies before applying model output.
Interactive work and background work must make progress.
Failed processing keeps the previous accepted result.
Saved model output can be retried for storage without a new paid call.

The earlier event-bus design and Phase 2.0 are historical material.
Phase 2.0 was rolled back. It is not the basis for new work.

## 10 Roadmap

Each phase delivers usable work. Its acceptance scenarios test the complete product path.
More use supplies more data for later phases.
The sequence keeps the earlier roadmap and adds Phase 3.9 before Phase 4.
It does not assign a new development batch.
The earlier phases come from the [earlier whitepaper](https://github.com/soaringjerry/PCAS/blob/c9ac3fd/docs/whitepaper.md#16-路线图).

[Project Status](status.md) gives release records, implemented functions, and known gaps.
A recorded release does not close an unresolved quality finding.
The roadmap's acceptance column states targets. It does not report fresh test results.

```mermaid
flowchart LR
    A[1 Secretary] --> B[1.5 Stabilization]
    B --> C[2 / 2.5 / 2.6 Memory core]
    C --> D[3 / 3.5 / 3.6 Workspaces and actions]
    D --> I[3.9 Foundation]
    I --> E[4 Observation panel]
    E --> F[5 Inputs, phone, and model selection]
    F --> G[6 Butler]
    G --> H[7 Training and model feedback]
```

### Recorded Phases

| Phase | Delivery scope | Acceptance target | Recorded state |
|---|---|---|---|
| 1 Secretary | One request executes actions. Undo, reminders, and notification channels are available. | Arrange a Friday meeting. The phone receives the reminder. Undo reverses the action. | Release recorded. Device checks have open findings. |
| 1.5 Stabilization | Examine state sequences. A different reviewer tests behavior. Repair defects and test live use. | State sequences pass. Live scenarios pass. One day of user use has no blocking defect. | Fixes recorded. Live and device acceptance have gaps. |
| 2 Memory query planning | Extract entities, places, and time. Plan retrieval. Use recall cases with conflicting or unrelated sources. | Recall the Chengdu intention with its source. Recall scores meet the agreed threshold. | Release recorded. The real-model recall baseline is open. |
| 2.5 Memory organization | Group memory. Compare and combine statements. Produce current context. Use task-appropriate memory and output review. Use versioned rules and bounded reprocessing. Expand task datasets with human review. | A model without prior conversation completes work from the handover and dates. Doing scores meet the agreed threshold. | Release recorded. Quality evidence has scope limits. |
| 2.6 Current-state correction | Remove per-group status cards. Keep sourced dates and requirements. Regenerate handovers from changed input. | Current context has sources. Changes update the applicable view. | Release recorded. This corrects Phase 2.5; it does not restore removed cards. |
| 3 Workspaces | First batch: project handovers, document versions, differences, and deputy context. Second batch: files, effort estimates, start dates, and plan timelines. | In ten seconds, identify the project conclusion, blockers, and next action. Revise a selected document version with displayed differences. | Release recorded. The ten-second target is not a measured result here. |
| 3.5 Automatic work | Create tasks, ideas, and projects from applicable sources. Show timed work and untimed progress. Give receipts and undo. | A meeting request appears on the home page. Imported tasks appear with undo. A substantial request becomes a project with steps. | Release recorded. Automatic project quality has open findings. |
| 3.6 Secretary cleanup | Show applicable timed entries. Close dates. Review obsolete dates. Convert actionable dates to tasks. Record cleanup with undo. | A canceled class leaves the current view. An obsolete journey stops appearing. Cleanup has a receipt and can be reversed. | Release recorded. Background judgment and check-mode replies have open findings. |

Phase 2.0 was rolled back. It is not Phase 2 query planning in this roadmap.
Recorded phase scopes explain delivered work. Current product rules apply to future changes.
Calendar conflicts belong to Phase 5. Reusable methods and completion judging stay in [Open Issues](tasks/backlog.md).

### Planned Phases

| Phase | Delivery scope | Acceptance target |
|---|---|---|
| 3.9 Foundation | System architecture, domain boundaries, model gateway, prompt registry, activity queries, and removal of obsolete paths. | Locate a result error in its business path. Change that path within its declared boundaries. Recover failed writes. Measure calls, cost, and duration. |
| 4 Observation panel | Visual activity, cost, model calls, memory management, access, input status, failures, and recovery. | Show yesterday's work, cost, and reasons. Trace a result to its source and model call. |
| 5 Inputs and model selection | Email, calendar, continuous Yufolo input, and IM trials. Phone voice, notifications, location, and microphone inputs. Automatic model selection. | A school email creates a workspace with materials and a start suggestion. Library arrival produces an applicable study suggestion. |
| 6 Butler | Habit memory, daily recommendations, budgets, subscriptions, income, expenses, and correspondence drafts. | After two pizza rejections, matching recommendations decrease. The observation panel shows the changed preference. |
| 7 Training and model feedback | Prepare datasets. Fine-tune local models. Compare quality and cost. Deploy accepted models with rollback. | A personal model handles intent interpretation with lower cost and no accuracy loss. |

### Phase 3.9 Foundation

Phase 3.9 makes the existing system understandable and changeable before further product repairs.
Its scope covers existing production paths, their dependencies, tests, and documents.
Business workflows leave the storage package. Each operation and rule has a responsible domain.
Model calls, prompts, actions, and costs have traceable identities.

Each business object has one owner. Other domains request changes and receive executed, skipped, or failed receipts.
Model stages supply proposals. The responsible domain validates and applies them.
Home and timeline queries only read business data. Skips, deferrals, and fallbacks have visible states and reasons.

Events record work and trigger declared specialist processing through the existing queue.
They carry trigger sources and causal identities. They do not make business decisions or select policy.
Owners record changes and their events within one transaction. A central response table names the applicable specialists.
Each event type has measured limits, visible overflow, and recovery to control repeated processing and model calls.

The system explains both an incorrect action and a missing expected action.
This includes why an expected project was not created, from source processing through candidate selection and execution.
Obsolete interfaces, duplicate implementations, and unsupported compatibility branches have explicit removal decisions.

During this phase, stop feature expansion and separate symptom patches.
Keep reported project, timeline, and task errors as investigation cases with their sources and observed results.
Complete the architecture work before assigning their business repairs.
Record existing defects separately from defects introduced by migration.
An existing incorrect output is not a new product requirement.

Complete the architecture, owner-contract, recorded-replay, concurrency, and real-model test layers within this phase.
The [architecture test contract](architecture.md#five-test-layers) gives their order, evidence, and credential boundaries.

Keep transaction, version, budget, recovery, access, and undo guarantees during migration.
Use the same data and model configuration to compare calls, context size, cost, duration, and output quality.
Investigate repeated processing, unnecessary calls, and scheduling delays from measured evidence.
Set numerical targets from that baseline. Keep estimated costs and missing measurements explicit.

The [System Architecture](architecture.md#10-foundation-acceptance) gives the complete acceptance criteria.
Its activity query supplies the basis for Phase 4.
Phase 4 still includes visual explanations, management, and recovery controls.

Phase 5 uses separate batches. Input work and phone work can proceed in parallel.
Each input needs platform verification. Phone collection uses device permissions.
Calendar input supplies the basis for calendar conflict checks.

Phase 7 follows accumulated use because training needs representative personal data.
Its acceptance compares measured quality and cost against an agreed baseline.
Personal-state models, digital twins, and decision assistance are long-term targets without an assigned phase.

## 11 Product Acceptance

Acceptance follows the complete input-to-result path with the applicable real model channel.
Tests of individual functions help this check. Model quality must also have complete scenario checks.

| Scenario | Required result |
|---|---|
| Arrange work in one sentence | Time, project, and reminder agree. Refresh keeps the result. Undo removes the action. |
| Answer while recording a second request | The answer and action succeed without a mode switch. |
| Recall the Chengdu intention | Sources, statement time, and subsequent changes are correct. |
| Return to a workspace | The conclusion, blockers, and next action reflect project information. |
| Revise an earlier document | A new version uses the requested base and shows the difference. |
| Prepare an assignment | The workspace, material, deadline, and start suggestion agree. |
| Send an email | The user sees the draft and confirms before sending. |
| Learn a preference | Rejected suggestions affect subsequent recommendations. |
| Recover from failure | Previous work stays available. The failure has a reason and a recovery action. |
| Examine team work | The user can trace the action, sources, model call, and cost. |
| Work without repeated background | A newly selected model receives the related current information. |

Some scenarios depend on future functions. Inclusion in this table does not mean that a scenario has passed.
