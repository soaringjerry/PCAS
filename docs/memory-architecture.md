# PCAS Memory Architecture

Current memory specification. Updated 2026-10-08.

The core preserves sources, interprets changes, retrieves information, and supplies current context to the team.
Product requirements are in the [Whitepaper](whitepaper.md#3-memory-core).
Code paths and HTTP interfaces are in the [Service Reference](memory-service.md).
[Project Status](status.md) lists implementation gaps.

## 1 Layers and Identity

| Layer | Stores or produces | Identity |
|---|---|---|
| Context | Original messages, files, media, and conversation context. | Source ID and version. |
| Structured | Entities, statements, evidence, relationships, and time. | Record ID and version. |
| Vector | Semantic indexes of stored text. | Record reference, provider, model, and dimension. |
| Current state | Handovers, dates, and user requirements. | Source dependencies and processing-rule version. |

Original media and parsed representations remain distinct.
Vectors, summaries, and handovers are derived data. They must not create an independent fact store.
A workspace defines a scope in the shared core. Relevant retrieval can extend outside that scope.

## 2 Structured Objects

| Object | Meaning |
|---|---|
| Entity | A person, place, organization, object, or project with a stable ID. |
| Alias | Another name for an entity, supported by evidence. |
| Episode | A related experience or discussion with linked messages and files. |
| Statement | An independently correctable fact, preference, intention, plan, or decision. |
| Evidence | A source location or execution record that supports or contradicts a statement. |
| Relationship | An evidenced connection, such as membership, dependency, change, or correction. |
| Group | A scope for a person, project, topic, domain, or type of personal information. |
| Date entry | A deadline, appointment, recurring arrangement, or unresolved date with a source. |
| User requirement | A lasting instruction about team behavior, with an applicable scope. |
| Habit | A pattern with evidence, a statistical period, uncertainty, and applicable time. |

One statement can belong to several groups.
Similar names do not prove that two entities are the same.
Statements keep their subject, value, scope, expression type, evidence, and time.
Memory confidence, activity, and task status are separate properties.
Habit statistics and advanced episode association remain targets where the code does not supply them.

## 3 Time and Change

The core distinguishes three time axes:

- Event or validity time: when an event occurs or a statement applies.
- Statement time: when the source expresses the information.
- Record time: when PCAS stores the information.

Unknown times and time ranges remain explicit.
A late import must not replace newer information solely because its record time is later.
New information can repeat, supplement, change, or correct an earlier statement.
Compatible statements can coexist. Repeated evidence can strengthen one statement.
A replaced statement leaves the current view but remains available in history.
A correction preserves its reason and source.
Quotation, uncertainty, negation, and speaker identity remain part of the meaning.
An AI reply can explain context. It does not become the user's decision.

## 4 Input and Processing

The system first saves the source, its identity, its version, and the required work event.
The source write and work event use one transaction.
Parsing, chunking, extraction, indexing, and vector generation can proceed asynchronously.
Duplicate source versions do not create duplicate records.
Unprocessed sources remain available for retrieval with a visible coverage gap.

Extraction uses surrounding conversation when necessary. Each statement and relationship has evidence.
Branches and speaker roles remain distinct. Shared branch content needs duplicate control.
Comparison selects candidates from related scopes and entities.
The model determines semantic repetition, compatibility, replacement, and alias identity.
The program validates identities, types, dates, references, and output structure.
String similarity alone must not determine a semantic decision.

Processing rules have versions. Rule changes identify affected records for bounded reprocessing.
An interruption must not lose the remaining work or require a complete restart.

## 5 Retrieval

| Purpose | Context requirements |
|---|---|
| Continue work | Current objects, applicable requirements, task state, and related sources. |
| Recall a past statement | Statement time, entities, expression type, original context, and later changes. |
| Read complete history | Linked sources and versions, branches, pagination, and coverage gaps. |

The query plan separates explicit conditions from uncertain hints.
Structured retrieval uses reliable conditions. Text and vector retrieval supplement the result.
Uncertain conditions can be relaxed with a visible explanation.
The system checks source text and current versions before supplying working context.

For the Chengdu example, "last year" describes statement time when the user asks what they said last year.
Place, subject, and intention type select relevant statements.
Related conversation can supply content that does not repeat the place name.
Completed and canceled intentions remain visible as history.

Lexical and vector scores have different scales.
The team path combines their ranks where rank fusion applies.
The public fallback path must not claim the same ranking without a code check.

Retrieval limits cover candidates, relation edges, context size, and traversal depth.
Each limit has a reason, an overflow path, and a count.
A fixed candidate count must not claim complete historical coverage.
Activity affects exposure and ranking. It does not exclude a memory from explicit recall.

## 6 Context for Work

Applicable requests receive the global handover, relevant dates, and global requirements within the declared limits.
Scoped requirements enter through relevant retrieval.
A handover includes its update time. Newer source information takes precedence over an older handover.
Source content and remembered requirements do not authorize unrelated work.

Context depth uses the cost of an error, task scope, user needs, and available time.
The existing light, medium, and heavy modes support different amounts of retrieval and review.
The service records the mode, groups read, omitted content, and failed reads.

The system can expand evidence into nearby conversation.
It preserves speaker roles, statement time, source version, and branch identity.
An excerpt is not a complete conversation. Missing context must not be filled with invented details.

The team shares the core by default. User access controls remain available.
External principals use explicit grants.
The system checks access, scope, and affected versions before applying generated work.

## 7 Current State and Dates

Current state comprises global and project handovers, a date table, and scoped user requirements.
It does not use the removed per-group status cards.
Project handovers use current memory, tasks, dates, current document versions, and deputy results.
Each handover statement has a source.
Affected changes invalidate the applicable view. Unchanged input does not trigger an unnecessary rewrite.
During regeneration or failure, the previous valid handover remains available with its update time.
Corrections apply to the underlying memory or task.

Date storage and date display have separate responsibilities.
An extracted date can describe a past event, quotation, consideration, or active commitment.
The home schedule displays applicable timed work.
Unresolved dates and arrangements without a usable time remain outside the home timeline.
Closing a date marks the applicable memory version as done, dropped, or transferred to a task.
The original memory remains available. Undo restores date state when its dependencies permit restoration.
Background date review records its decision and reason. Uncertain results preserve the previous state.

## 8 Consistency and Recovery

Structured writes use version checks and atomic events.
Workers use leases and fencing tokens. A worker that loses its lease cannot commit.
Generated work is checked against affected dependencies before application.
An unrelated change must not invalidate the complete request without a relevant dependency.

Each model stage has its own call budget.
The system records deferred work and the reason for deferral.
Interactive and background work must both continue to make progress.
Failed calls or invalid output must not overwrite valid data or mark unfinished work as complete.
Saved model results can be retried for storage without another model call.

Deletion clears selected records and affected copies, including training exports where applicable.
A source outside the selected deletion scope remains stored.
Minimal markers prevent blocked content from returning through import.
Corrected data must not revert when an old archive is imported again.

## 9 Activity and Learning

User mention, confirmation, and adoption can strengthen activity within configured limits.
System retrieval, display, and repetition do not count as user adoption.
Decay affects exposure. It does not delete a fact or complete a task.
Reminder state is separate from memory activity.
Habit statements retain evidence and a statistical period.
User feedback can revise a habit. A lack of undo is not an automatic correctness label.
Training samples retain sources and versions.

## 10 Verification

Verify recall, current work, and correction as complete paths.
Use wrong dates, similar names, quotations, later changes, missing media, and unrelated content as interference.
Measure evidence recall, object errors, state errors, correction effects, context size, and response time.
Use representative group sizes and concurrent input, scheduling, and user requests.
Inspect real model output when a change affects context, prompts, or output format.
The [Whitepaper](whitepaper.md#11-product-acceptance) defines product scenarios.
Existing reports establish their recorded scope only.
