# PCAS Documentation Writing Guide

Current writing rules. Updated 2026-10-08.

Current specifications and operating instructions use English.
Historical records keep their recorded language, results, and evidence.
User communication uses Chinese. Code and commit messages use English.

## 1 STE100 Basis

Use ASD-STE100 Issue 9 for English technical writing.
The standard includes writing rules and an approved general vocabulary.
Project technical terms supplement that vocabulary in the standard's term categories.
Sentence length is one part of STE100. Vocabulary, grammar, and meaning also have rules.

- Use no more than 20 words in a work-step sentence.
- Use no more than 25 words in a descriptive sentence.
- Give each instruction its own sentence unless actions must occur together.
- Put a necessary condition before its action.
- Identify the person or system that acts. Use an active verb.
- Keep articles and other words that are necessary for meaning.
- Do not use contractions or ambiguous pronouns. Divide long noun clusters.
- Use one subject and no more than six sentences in each paragraph.
- Use each term with the same spelling, meaning, and grammatical role.

[Official standard](https://www.asd-ste100.org/assets/files/ASD-STE100_ISSUE9.pdf).
[Official explanation](https://asd-ste100.org/about_STE.html).
These limits apply to prose. Commands, code identifiers, URLs, and exact API fields keep their necessary syntax.

## 2 Project Terms

The terms below are technical nouns unless a verb use is specified.
Code names and literal UI labels keep their existing spelling.

| Term | Meaning |
|---|---|
| PCAS | Personal Central AI System. |
| Memory core | The shared storage, interpretation, retrieval, and correction system. |
| Context layer | Sources and their surrounding information. |
| Structured layer | Records of entities, statements, evidence, relationships, and time. |
| Vector layer | Semantic indexes that refer to stored sources and records. |
| Current-state layer | Derived handovers, dates, and applicable requirements. |
| Source | A message, file, media item, or execution record received or produced by PCAS. |
| Accepted result | Stored output that passed its software checks. This status does not prove factual accuracy. |
| Statement | An independently correctable unit of remembered meaning. |
| Claim | The code and database term for a statement. |
| Evidence | A reference with information for or against a statement. |
| Scope | The data range or applicability of a record or requirement. |
| Workspace | The project space for memory, tasks, files, and deputy work. Lab is a different product name for the same space. |
| Handover | Derived background for the team or one project. |
| Secretary | The role that receives requests, answers, acts, and delegates. |
| Deputy | The role that prepares substantial work for the user. |
| Butler | The role that helps with habits, daily choices, and household administration. |
| Observation panel | The interface for explanations, memory management, costs, and recovery. |
| Receipt | The recorded result of an executed, skipped, or failed action. |
| Recall | Retrieval of related memory; also a technical verb for this operation. |
| Undo | Restoration through a recorded action; also a technical verb. |
| Agent | The model configuration used by a team role in the implementation. |
| Connector | An input configuration for webhook, polling, or folder data. |
| Archive | A file that contains exported conversation records. |
| Import | Transfer of external records into PCAS; also a technical verb. |
| Export | Transfer of selected stored records out of PCAS; also a technical verb. |
| Lease | A worker's temporary right to process a job. |
| Fencing token | The identity that prevents an expired worker from committing. |
| Rank fusion | Combination of retrieval rankings calculated independently. |
| Prompt | The instructions and context supplied to a model. |
| Prompt registry | The catalog of model instructions, templates, and their content hashes. |
| Model gateway | The application entry for provider calls, accounting, capability checks, and call records. |
| Domain | A group of business responsibilities with explicit interfaces. |
| Invocation | One attempted model call, with its own identity and outcome. |
| Execution | One business request or background operation that can contain several invocations and actions. |
| Event | A recorded occurrence that identifies its trigger source and cause. Declared event types can trigger specialist processing. |
| Causal chain | Linked processing and events that share one root execution and its resource counters. |
| Event-response table | The authoritative mapping from event types to named specialists, input dependencies, queue stages, and limit policies. |
| Input manifest | The recorded identities, versions, and processing details for data supplied to a model. |
| Read model | A query representation of stored records for a specific reading purpose. |
| Model token | A text unit counted by the selected model. |
| API token | A credential for API authentication. |
| Database schema | The structure of database objects. |
| Output schema | The structure of model output. |
| Git commit | A saved repository revision. |
| Transaction commit | The atomic completion of a database transaction. |
| API, HTTP, SQL, JSON, UUID, OCR, GPS, IM | Established interface, data, and input names. |
| PostgreSQL, pgvector, Go, Codex, Playwright, Chromium | Exact product and tool names. |

The following terms also name software objects or engineering concepts:

- Source ID, source version, source original, message branch, statement time, and event time.
- Database transaction, query plan, retrieval scope, rank fusion, memory activity, and adoption record.
- Model provider, model channel, model output, call budget, and background stage.
- Action receipt, request identity, access grant, processing status, and coverage gap.
- Code review, acceptance check, deployment record, and backup file.

Other exact technology names and identifiers are technical nouns in their stated domain.
Technical verbs below name software operations. Use them only with the stated software object or process.

| Technical verb | Operation |
|---|---|
| Create, update, delete | Change a software record or object. |
| Save, store, load | Transfer data to or from persistent storage. |
| Parse, extract, index | Convert source content into software records and indexes. |
| Retrieve, query, filter, search | Read selected software records. |
| Import, export | Transfer records across the system boundary. |
| Execute, run, retry, resume | Start or continue a software command, job, or action. |
| Validate, invalidate | Test a software object against its constraints, or mark it as obsolete. |
| Commit, merge | Complete a database write or repository operation. Qualify the object. |
| Return, generate, render | Supply a software response, model output, or interface representation. |
| Undo, restore | Apply a recorded software reversal or backup. |

An ordinary verb does not become approved merely because it occurs in a software document.

## 3 Meaning and Status

Identify who acts, what changes, and when the rule applies.
Identify product targets, implemented functions, deployment evidence, and measured quality with different status labels.
For a number, check the rule and code path before writing it.
For a limit, include its source, reason, overflow behavior, and count when known.
If a reason is unknown, identify that gap. Do not invent a reason.

Keep one authoritative location for each rule.
Replace duplicate requirements with a link.
Mark historical task instructions and research as historical material.
Do not shorten a rule by removing an exception, dependency, or recovery condition.
Use tables for comparisons and diagrams for relationships.

## 4 Review

1. Read the governing requirement and implementation reference.
2. Make sure that the text keeps its conditions and meaning.
3. Examine the approved vocabulary and technical terms.
4. Examine word meanings and parts of speech.
5. Examine sentence length, grammar, and paragraph scope.
6. Examine links, headings, and status labels.
7. Read the result without historical material to check that current instructions are complete.

Mechanical checks help the review. A reviewer must also examine word meanings, grammatical roles, and permitted technical terms.
