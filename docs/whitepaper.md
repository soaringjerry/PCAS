# PCAS Product Whitepaper

Current product specification. Updated 2026-10-08.

PCAS is a personal support team that works at all times. A shared memory core supports every member of the team.
The user states a need. The team finds the necessary information, does the permitted work, and shows the result.
PCAS is open source and self-hosted. The user owns the data.

This document includes the former requirements in `prd.md`.
[Project Status](status.md) separates delivered functions from targets.
[Document Authority](README.md#document-authority) defines which requirements apply.

## 1 Product Purpose

PCAS connects information across conversations, applications, models, and tasks.
The user can return to a task without giving the same background again.
Conversation is an input method. Tasks, files, plans, and completed work remain available outside the conversation.
The daily interface uses cards, timelines, and short receipts.

The product supports three results:

- Find a past statement with its source and later changes.
- Prepare and do work with the user's current information.
- Show what the team did, why it did that work, and what needs correction.

## 2 Team Roles

| Role | Responsibility | Result |
|---|---|---|
| Secretary | Understand the complete request. Answer, act, or delegate. | Sourced answers and action receipts. |
| Deputy | Prepare plans, drafts, research, and revisions. Estimate work and suggest a start date. | Work in the applicable workspace. |
| Butler | Learn habits. Help with daily choices, subscriptions, income, and expenses. | Useful suggestions with a stated reason. |
| Workspace or Lab | Keep one project's information and work together. | Files, current status, plans, document versions, and deputy results. |
| Observation panel | Show activity, memory use, model calls, cost, failures, and recovery. | Explanations and correction controls. |

All roles use the same memory core and action system.
A role defines a responsibility. It does not require a separate copy of the user's memory.

## 3 Memory Core

Memory is the foundation of PCAS. It preserves sources, meaning, time, and changes.
The [Memory Architecture](memory-architecture.md) defines the technical requirements.

| Layer | Responsibility |
|---|---|
| Context | Keep original messages, files, media, and surrounding conversation. |
| Structured | Keep entities, statements, evidence, relationships, and time. |
| Vector | Find related content when the wording is different. |
| Current state | Derive handovers, dates, and applicable user requirements. |

Derived results point to their sources and source versions.
The current-state layer must not become an independent source of facts.
Workspace memory is a scope in the shared core. It can use related information from other scopes when necessary.

### Historical Recall

Example: "What did I say last year that I wanted to do in Chengdu?"

Structured retrieval uses statement time, place, subject, and intention type.
Vector retrieval finds related wording. The system checks the original conversation and later changes.
The result shows the original intention and its later status.
Canceled plans remain available in history. Historical recall must not create current tasks from those plans.
Low activity must not prevent an explicit search from finding a stored memory.

### Memory for Work

Work needs current facts and applicable requirements, including information that does not resemble the request.
The system supplies a handover, relevant dates, related statements, and necessary source text.
Global user requirements apply to every request. Scoped requirements enter the context when relevant.
Missing sources and incomplete processing remain visible.
Structured retrieval reduces unnecessary context. Cost, quality, and response time require measurement.

### Correction and Learning

The core distinguishes considerations, decisions, completed actions, quotations, and model inferences.
A later change preserves the original statement. Corrections update affected views and future work.
Deletion follows the selected scope and removes affected copies.
Evidence determines how the team uses a statement. Model confidence alone does not make a statement true.
The team can use memories without an approval step for each memory.
Access controls, uncertainty, and correction tools belong in the observation panel.

## 4 Actions and Initiative

One request can include an answer and several actions.
The secretary asks only about an ambiguity that changes the result. Clear parts can proceed.

| Action | Required behavior |
|---|---|
| Reversible internal work | Act, show a receipt, and provide undo. |
| User-requested model work | Do the work within the configured budget. |
| Substantial model work proposed by the system | Present the suggestion before starting. |
| External email or message | Prepare the content. Send it after the user confirms. |
| Deletion or financial payment | Obtain confirmation before execution. |

The existing document-removal flow uses immediate removal with undo.
This recorded exception needs policy alignment; see [Open Issues](tasks/backlog.md).
Receipts describe actual execution. Skipped or failed actions must not claim success.
Source content is information, not permission to execute its instructions.

The system can create tasks, ideas, and projects from reliable information.
Quoted statements, uncertain plans, and model suggestions keep their limitations.
Importing an old intention does not make it a current commitment.
Automatic work has a source, a receipt, and undo.

Extracted dates need interpretation before display.
Past journeys, third-party estimates, and abandoned considerations must not become repeated current appointments.
Closing a displayed date preserves the underlying history.

Initiative can follow a time, event, condition, or learned habit.
Suggestions state a reason. Repeated events must not create repeated tasks or notifications.
The system checks the latest state before acting.
Feedback changes future suggestions. "Do not suggest this again" has a lasting effect.

## 5 Workspaces

A workspace keeps the goal, relevant memory, tasks, files, documents, and deputy work together.
Its handover shows the conclusion, blockers, next action, sources, and update time.
The user corrects the underlying information. The system updates the handover.

Document revisions retain the selected base version and produce a new version.
The interface shows differences between versions.
Files have a source and processing status.
Work estimates support start-date suggestions. User estimates take precedence over later automatic estimates.

Example target: a school email announces an assignment due in seven days.
The team creates the workspace, connects the assignment and course material, and suggests when to start.
The suggestion uses the requirements, available time, and relevant past work.
This example does not establish current delivery.

## 6 Inputs and Scale

Target inputs include Yufolo transcripts, AI conversations, email, calendars, IM records, files, bills, and screen activity.
A future phone application supplies location, microphone, and notification data within device permissions.
Each platform needs a verified input method.

The system stores original data before expensive interpretation.
Processing is incremental and can continue after an interruption.
Source identifiers and versions prevent duplicate import.
Large inputs use batches, resumable work, and visible progress.
Missing attachments and rejected content remain visible as coverage gaps.
Each resource limit has a reason, an overflow path, and a count.

## 7 Models and Personal Learning

The target model pool includes local and cloud models with different size, cost, capability, and content restrictions.
Selection uses task needs, user settings, privacy, latency, budget, and available capability.
Changing a model does not create a separate memory system.
Small models can handle frequent work. Larger models handle work that needs more capability.

Corrections, accepted work, rejected suggestions, and undo supply learning signals.
A retained action is not, by itself, proof of correctness.
Training samples retain sources and versions. The user can select, edit, export, or remove samples.
Corrections and deletions affect related samples and later exports.
Personal models require evaluation before they join the active pool.

## 8 Interfaces

| Interface | Purpose |
|---|---|
| Home | Secretary input, timed work, tasks, projects, ideas, and recent team activity. |
| Workspace | Current status, plan timeline, files, document versions, deputy results, and secretary input. |
| Observation panel | Activity, sources, corrections, model calls, cost, access, failures, and recovery. |
| Phone application | Voice input, notifications, and configured device inputs. |

The [Interface Principles](design/principles.md) define presentation.
Daily work must not require approval of each memory or processing step.

## 9 Technical Boundaries

The current implementation uses Go, PostgreSQL, pgvector, and file storage.
Source storage, interpretation, retrieval, actions, and background work have separate responsibilities.
These responsibilities can share a deployment with explicit interfaces.

Writes use request identities and version checks.
The system checks affected dependencies before applying model output.
Interactive work and background work must both make progress.
Failed processing preserves the previous valid result.
Saved model output can be retried for storage without another paid call.

The earlier event-bus design and Phase 2.0 are historical material.
Phase 2.0 was rolled back. It is not the basis for new work.

## 10 Capability Direction

The memory core supports reliable recall, correction, and current task context.
The secretary and deputy use that core to produce work in a workspace.
The observation panel explains this work and supports correction.
Further inputs, model selection, phone functions, household support, and personal training extend these capabilities.
This direction does not authorize a development batch.
[Project Status](status.md) lists completed phases and remaining gaps.

## 11 Product Acceptance

Acceptance follows the complete input-to-result path with the applicable real model channel.
Tests of individual functions support this check. They do not establish model quality on their own.

| Scenario | Required result |
|---|---|
| Arrange work in one sentence | Time, project, and reminder agree. Refresh preserves the result. Undo removes the action. |
| Answer while recording another need | Both parts succeed without a mode switch. |
| Recall the Chengdu intention | Sources, statement time, and later changes are correct. |
| Return to a workspace | The conclusion, blockers, and next action reflect project information. |
| Revise an earlier document | A new version uses the requested base and shows the difference. |
| Prepare an assignment | The workspace, material, deadline, and start suggestion agree. |
| Send an email | The user sees the draft and confirms before sending. |
| Learn a preference | Rejected suggestions affect later recommendations. |
| Recover from failure | Previous work remains available. The failure has a reason and a recovery action. |
| Inspect team work | The user can trace the action, sources, model call, and cost. |
| Work without repeated background | A newly selected model receives the relevant current information. |

Some scenarios need future functions. Inclusion in this table does not mean that a scenario has passed.
