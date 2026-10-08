# PCAS Documentation Writing Guide

Current writing rules. Updated 2026-10-08.

Current specifications and operating instructions use English.
Historical records keep their original language, results, and evidence.
User communication uses Chinese. Code and commit messages use English.

## 1 STE100 Basis

Use ASD-STE100 Issue 9 for English technical writing.
The standard includes writing rules and an approved general vocabulary.
Project technical terms supplement that vocabulary within the standard's term categories.
A short sentence alone does not establish STE100 conformity.

- Keep a work-step sentence within 20 words.
- Keep a descriptive sentence within 25 words.
- Give each instruction its own sentence unless actions must occur together.
- Put a necessary condition before its action.
- Prefer an explicit actor and an active verb.
- Retain articles and other words needed for meaning.
- Avoid contractions, ambiguous pronouns, and unnecessary noun chains.
- Keep paragraphs on one subject, within six sentences.
- Keep a term's spelling, meaning, and grammatical role consistent.

[Official standard](https://www.asd-ste100.org/assets/files/ASD-STE100_ISSUE9.pdf).
[Official explanation](https://asd-ste100.org/about_STE.html).
These limits apply to prose. Commands, code identifiers, URLs, and exact API fields keep their required syntax.

## 2 Project Terms

The terms below are technical nouns unless a verb use is specified.
Code names and literal UI labels retain their existing spelling.

| Term | Meaning |
|---|---|
| PCAS | Personal Central AI System. |
| Memory core | The shared storage, interpretation, retrieval, and correction system. |
| Context layer | Original sources and their surrounding information. |
| Structured layer | Records of entities, statements, evidence, relationships, and time. |
| Vector layer | Semantic indexes that refer to stored sources and records. |
| Current-state layer | Derived handovers, dates, and applicable requirements. |
| Source | An original message, file, media item, or execution record. |
| Statement | An independently correctable unit of remembered meaning. |
| Claim | The code and database term for a statement. |
| Evidence | A reference that supports or contradicts a statement. |
| Scope | The data range or applicability of a record or requirement. |
| Workspace | The project space for memory, tasks, files, and deputy work. Lab is an alternate product name. |
| Handover | Derived background for the team or one project. |
| Secretary | The role that receives requests, answers, acts, and delegates. |
| Deputy | The role that prepares substantial work for the user. |
| Butler | The role that supports habits, daily choices, and household administration. |
| Observation panel | The interface for explanations, memory management, costs, and recovery. |
| Receipt | The recorded result of an executed, skipped, or failed action. |
| Recall | Retrieval of relevant memory; also a technical verb for this operation. |
| Undo | Restoration through a recorded action; also a technical verb. |
| Agent | The model configuration used by a team role in the implementation. |
| Connector | An input configuration for webhook, polling, or folder data. |
| Archive | A file that contains exported conversation records. |
| Import | Transfer of external records into PCAS; also a technical verb. |
| Export | Transfer of selected stored records out of PCAS; also a technical verb. |
| Lease | A worker's temporary right to process a job. |
| Fencing token | The identity that prevents an expired worker from committing. |
| Rank fusion | Combination of separate retrieval rankings. |
| Prompt | The instructions and context supplied to a model. |
| Token | A model text unit or an authentication credential. Specify which meaning applies. |
| Schema | A database structure or model-output structure. Specify which meaning applies. |
| Commit | A Git revision or an atomic database write. Specify which meaning applies. |
| API, HTTP, SQL, JSON, UUID, OCR, GPS, IM | Established interface, data, and input names. |
| PostgreSQL, pgvector, Go, Codex, Playwright, Chromium | Exact product and tool names. |

Other exact technology names and identifiers are technical nouns in their stated domain.
An ordinary verb does not become approved merely because it occurs in a software document.

## 3 Meaning and Status

State who acts, what changes, and when the rule applies.
Separate a product target from code support, deployment evidence, and verified quality.
For a number, check the rule and code path before writing it.
For a limit, include the source, reason, overflow behavior, and count where established.
If a reason is not established, identify that gap instead of inventing one.

Keep one authoritative location for each rule.
Replace duplicate requirements with a link.
Mark old task instructions and research as historical material.
Do not shorten a rule by removing an exception, dependency, or recovery condition.
Use tables for comparisons and diagrams for relationships.

## 4 Review

1. Read the governing requirement and implementation reference.
2. Check that the text preserves its conditions and meaning.
3. Check the approved vocabulary and technical terms.
4. Check word meanings and parts of speech.
5. Check sentence length, grammar, and paragraph scope.
6. Check links, headings, and status labels.
7. Read the result without historical material to check that current instructions are complete.

Mechanical checks support review. They do not prove vocabulary meanings or complete STE100 conformity.
