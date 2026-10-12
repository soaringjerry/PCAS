# Foundation Domain Pilot: Workspace Effort

Written by the coordinator on 2026-10-11.
This page records the first business path that left `internal/postgres`. It also gives the pattern for the paths that follow.
It follows the architecture's [target dependencies](../architecture.md#3-target-dependencies) and [migration rules](../architecture.md#9-migration-and-evidence).

## Why This Path

Effort estimation is a complete background path in about 280 lines.
It selects input, calls the model, saves a paid result, validates the output, changes workspace tasks, and recovers after a failure.
The architecture requires a pilot that uses each of these steps.

## Structure

```mermaid
flowchart LR
    Worker[Background worker] --> App[internal/app: handler table]
    App --> Service[internal/workspace/effort: Service]
    Service --> StorePort[Store and Unit interfaces]
    Service --> GeneratorPort[Generator interface]
    StorePort --> Adapter[internal/postgres: effortStore]
    GeneratorPort --> Background[internal/postgres: background generation]
    Background --> Gateway[Model gateway]
```

| Package | Contents |
|---|---|
| `internal/workspace/effort` | The start-date rule, the task plan rule, the estimation service, and the interfaces that it needs. It has no SQL and does not import the storage adapter. |
| `internal/postgres/effort.go` | The adapter. It holds the SQL, the job lease, the owner lock, and the transactions. It decides nothing about a task. |
| `internal/app` | Application setup. It constructs the service and holds the worker's handler table. The server and the replay tool use the same table. |

## Paths Outside the Storage Package

| Path | Domain package | Storage adapter | Still in storage |
|---|---|---|---|
| Effort estimation | `internal/workspace/effort` | `internal/postgres/effort.go` | The scheduler query and the plan rule's call from item saves. |
| Topic projects | `internal/workspace/topicproject` | `internal/postgres/topic_projects.go` | The input assembly, the scheduler, and undo. |
| Date review | `internal/currentstate/datereview` | `internal/postgres/date_tidy.go` | The input assembly, the scheduler, and the home page's list of reviewed dates. |
| Project handover | `internal/workspace/projecthandover` | `internal/postgres/project_handover.go` | The input assembly, the evidence check queries, the scheduler, and the reader for the project page. |
| Global handover | `internal/currentstate/handover` | `internal/postgres/status_handover.go` | The input assembly, the scheduler, and the stale rule in SQL. |
| Alias scan | `internal/interpretation/aliasscan` | `internal/postgres/entity_candidates.go` | The batch selection, the name checks of a proposed pair, and the scheduler. |
| Statement comparison | `internal/interpretation/comparison` | `internal/postgres/compare.go` | The batch plan, the write of accepted edges with its eligibility checks, the receipt, the scheduler, and the reads and the write of a rejudged pair. |
| Entity comparison | `internal/interpretation/comparison` | `internal/postgres/compare_entities.go` | The pair selection, the name and evidence checks, the merge, the receipt, and the scheduler. |
| Organize | `internal/interpretation/organize` | `internal/postgres/organize.go` | The batch selection, the write of labels, groups, dates and rule scope for each statement that is still current, and the scheduler. |
| Attachment parsing | `internal/input/attachment` | `internal/postgres/attachments.go` | The job, the lock of one original, the write of the derived text, and the receipts of paid readings. The archive import is not moved. |
| Vector index | `internal/retrieval` (`Indexer`) | `internal/postgres/processing.go` | The choice of the texts that have no vector yet, and the write of the vectors under the job's lease. |
| Voice transcription | `internal/input/voice` | None. | Nothing. The call is recorded by the gateway. |
| Extraction | `internal/interpretation/extraction` | `internal/postgres/processing.go` | The reads for the model input, the write of each decided item, the state of the source, and the preparation and the write of one segment of an imported conversation. |

The topic project service holds the proposal rules: a match must name a project that the model saw, a new project waits until the model has seen every project, an existing name attaches instead of creating, the user's own name for a project is kept, and four projects are created in one local day at most.
Its input assembly reads memory and clips evidence in one function with SQL. That function stays in storage until memory retrieval has its own interface.

The date review service holds the proposal rules for a remembered date that is not a time to show.
A task or an idea is created and the date is closed in one recorded action, through the workspace item command and the memory date command.
A kept date is recorded too, so that it is not reviewed again unchanged. A date that changed during the call gets no verdict.

The project handover service holds the grounding rules: a sentence is kept only when its evidence was in the input and still exists at the time of the write, a section keeps five sentences, and an output without a grounded sentence leaves the earlier handover in place.
It also holds the preparation rules: no handover for a finished project, no rewrite of a current handover, and one rewrite of a stale handover in one hour at most.

Rules that need no storage can move before their path's orchestration.
The statement comparison rules are in `internal/interpretation/comparison`: reading the model's duplicate and superseded proposals, and following chains of proposals to their final keeper without accepting a cycle.
The comparison job's flow is in the same package. Its write of accepted edges is still in `internal/postgres/compare.go`.

The organize service builds the model input from the batch that storage reads, and it reads the answer.
An item is accepted only when the whole item is valid. A declared group is created only when a statement that is still current uses it.
The package also holds the schedule rule of a recurring date. The schedule reader and the status dates use the same type.
The old code counted declared groups that were "ungrounded". The check could not fail, so the count was always zero. It is removed.

The attachment parser reads an original file by its media type. It holds the limits of a file, the page rule of a PDF file, and the fallback from a model reading to character recognition.
Storage gives it the paid calls through one interface and keeps each paid result with the file. The storage package no longer starts an external processor.
The storage code still holds its transaction open while the parser makes its calls. This order did not change.

The extraction rules are in `internal/interpretation/extraction`: the windows of a long source, the strict reading of the answer, the rules of the source's speaker, the quotation that each item needs, the confirmation level of a statement, and the event time.
The extraction job's flow is in the same package: which sources have nothing to extract, the split of a long source, the model input, and the handling of an answer that is not valid.
`Decide` gives the action for each item: nothing, a task or an idea, a candidate for review, or a statement. Storage applies the action. It no longer decides it.
The reads for the model input and the writes are still in storage.
The conversation path of an import has its rules in the same package: the split of a conversation into segments, the identity of a run, the stage of a segment job, and the decision for each item. Its job flow is still in `internal/postgres/conversation_extract.go`, because each step of that flow is a queue write.

The secretary's rules that need no storage are in `internal/workspace/secretary`: the reading of the answer, where one invalid action keeps its name and does not reject the others; the fixed classes of a failure; what the owner reads when a turn did not finish; and the due time and reminder of a task.
The rules of each action are in the same package. `Execute` does one action through the `Workspace` interface: it resolves the alias, checks the fields, sends the workspace commands, sets the reminder, and writes the receipt that the owner reads.
Storage implements `Workspace` in the action's transaction. It keeps the commands, the history collection, the companion project of a task, and the closing of a remembered date.
Behavior tests run the actions against a workspace in memory. They need no database.
`RenderWorkspace` writes the workspace part of the model input: the projects, the tasks, the ideas, the conversation history, and the items of the conversation. Storage reads the facts and records each cut of a long project evidence. A contract test fixes the bytes.
`RenderMemories`, `RenderExcerpts`, and `RenderEvidence` write the memory part of the model input and give each memory and each source its alias. Storage reads in two steps: first the recall and the current state, then the source context of the memories that were given an alias.
The rules for applying an answer are in the same package: ten actions in one turn at most, the receipt of an action whose fields were not read, the receipt of an action that did not finish, the alias of an item that an earlier action of the turn made, and which shown memories the owner took up or mentioned.
More rules of the turn are in the same package: the check of a request, what the self-check can change in an answer, the lines for memories that a reader adds, what became of each paid call, and what the owner reads when a turn did not finish.
`secretary.Apply` is the order in which a read answer is applied: the reply, the memories that the owner took up or mentioned, the actions with the items that earlier actions of the turn made, and the cards. It works through the `Turn` interface. Storage implements `Turn` in the transaction of the turn; one action runs in a transaction of its own there, so that the owner can undo it alone. Behavior tests run `Apply` against a turn in memory.
`secretary.Ask` is the order in which the model is asked: the heavy reading before the answer, the answer call, the reading that a deeper answer asks for, the rise of the tier, and the self-check with its time limit. It works through the `Model` interface. `secretary.Asker` implements `Model` with the readers and the gateway calls; storage gives it the checks of the turn's context. Flow tests run `Ask` against a model in memory.
`secretary.Conduct` is the whole turn: it reads the context, asks the model, checks that the targets of the answer are unchanged, and applies the answer. A turn that cannot finish keeps the owner's sentence and says why. Each paid call is recorded with what became of it. It works through the `Session` interface, which storage implements in the transaction of the turn. Flow tests run `Conduct` against a session in memory.
What is still in `deskStore.Turn` in `internal/postgres/desk_turn.go` is 165 lines of transport: the check and identity of the request, admission in order, the replay of a repeated request, the receipts of attachments, and the saving of the turn. The context reads behind `Session` are SQL and stay in storage.

The context builder is `internal/contextbuilder`. It serializes memory and current state for the model input of an agent turn: the handover, the requirements, the dates, the memories with their metadata, and the lines of a source context. The secretary and the deputy use the same rules.
Its five instruction sentences are registered prompts: `context-requirements`, `context-dates`, `context-groups`, `context-evidence`, and `context-recall-relaxed`. Its headings and data lines are serialization and stay in the package. A test fixes the bytes of the sentences.
It needs no storage. The caller reads the data and gives each memory its alias. The reads of the memory part are still in storage. The deputy still assigns its labels and its budgets in storage.

The rules that change one task, idea, or project by one command are in `internal/workspace/itemcommand`. `Apply` checks that the command fits the kind of the item, changes the item, and returns the summary for its history. Storage reads the item and saves it.
The same package holds three more sets of rules. `New` makes the item of a creation command and says who made it. `ReviewCandidate` applies the owner's decision to a candidate: accept, ignore, restore, or merge. `Expand` turns a bulk command into one command for each distinct item and refuses more than 100 items.
The rules for a change of the owner's settings and of an agent's settings are in `internal/workspace/settings`: the fields that can change, their limits, and when the task plans need a new calculation.
The commands for documents stay in `internal/postgres/commands.go`. Each of them is a patch and a write.

The G4 browser test failed once with `version_conflict`, and the cause is not established. One lead: the undo button sends its command again one time after a conflict, and two background writes in a row would make the second try conflict too. No log of the failure is kept, so this lead is not confirmed.

The deputy's rules that need no storage are in `internal/workspace/deputy`: the checklist of an output, how an output is adopted, the notice of a finished run, and the output format of a revision and a breakdown.
`deputy.Brief` builds the model input of a run. It writes the item with its project and its steps, the document that a revision starts from, and the current documents up to the limit of the brief. It counts each document that was cut or left out; storage records the count.
`Memories`, `Excerpts`, `Evidence`, `Derived`, and `Request` write the rest of the brief. They hold the limit of the brief: metadata on a memory line does not count, so that it cannot push out a later memory or source. Each method returns the count of parts that did not fit, and storage records one event with that count. Before, storage recorded one event for each part; the sums are equal.
The rules of the self-check are in the same package: a light run does not check its draft, the check gets the time of the answer and 30 seconds at most, and the checked text replaces the draft only when it kept the count of checklist entries. `SettledCost` gives the cost of an answer call whose outcome is not known.
`CheckAdoption` and `Subtasks` hold the rules of adopting a result: only a finished run whose context is still current is adopted, and only once; a revision becomes a document; a text gives one to one hundred subtasks.
The rules of the run commands are in the same package: the check of a request, the order of the memories that a run is given, with each preference and decision kept also when the task does not name it, and the taking or discarding of a result by hand.
`HeavyTask` and `RefreshMemory` hold what a run does with its brief when it starts: the task of the heavy reader, and the replacement of the memory part by its byte range.
The reads for the brief and the order of the run with its leases, its calls, and its recovery are still in `internal/postgres/runs.go`. What remains there is storage and accounting work. It moves with the call journals, not as rules.

The rules of a statement that is about to be written as a memory are in `internal/interpretation/claim`. `Normalize` checks that the statement has text, a known nature, and a source version, and it gives each absent field its one meaning. The same package gives the key of an assertion, the place of its quotation in the source, the time at which it was said, and the scope of the new memory. The write itself, with the reimport block, the evidence, the subject entity, and the versions, is still in `internal/postgres/claims.go`.

The memory retrieval rules that need no storage are in `internal/retrieval`: the budget of a recall with its defaults and limits, and the choice of the part of a source text that answers a query. The recall itself, with its structured, lexical, and vector reads and its access rules, is SQL and stays in `internal/postgres/retrieval.go`.

The chunk, index, and summary jobs stay in storage. They hold no interpretation rule: the text split is `memory.SplitText`, and the summary is a memory read. The embedding job left storage with its model call: `retrieval.Indexer` chooses the provider, makes the call, and decides what a failed call becomes for the queue. The card job only acknowledges queue slots from before an upgrade.

## Model Calls Outside the Storage Package

A storage package must not make a model call. The domain package owns the request, the call, and the meaning of each failure. Storage keeps the records of the call and the writes of the flow.
The test `TestStoragePackagesDoNotMakeModelCalls` in `internal/app/boundaries_test.go` enforces this rule: a storage package cannot refer to the gateway type or its constructor, and cannot call a method that submits a model call.
`internal/app.Gateway` builds the gateway over the store's call records (`Store.CallRecords`, `Store.DirectCallRecords`) and the configured models. The store holds no gateway.

All ten calls that were in the storage package on 2026-10-12 have an owner outside it.

| Call | Owner |
|---|---|
| The generation of each background job | `internal/modelcall/jobcall` (`Generator`). It maps each gateway result to the queue's failure. The storage adapter `Store.JobCallStore` keeps the paid results and the reservations. |
| The rejudge of a superseded pair | `comparison.Service`. Storage gives the pair and applies the verdict. |
| One segment of an imported conversation | `extraction.Service`. Storage prepares the segment and applies the answer. |
| The embedding of a source or a memory | `retrieval.Indexer`. |
| The transcription of a channel's voice message | `voice.Transcriber`. |
| The secretary's answer with its retry, and its self-check | `secretary.Asker`. Storage verifies the turn's context before a second answer call and before the check. |
| The memory readers of the secretary and the deputy | `internal/contextbuilder/reader` (`Service`). Storage gives the members and the memories of a group. |
| One stage call of an admitted execution | `internal/modelcall/stagecall` (`Caller`). The readers and the self-check use it. |
| The query embedding of a recall | `retrieval.Recaller`. `Store.RecallWith` checks the request first and then asks for the vector once. |
| The transcription and the image reading of an attachment | `attachment.Reader`. Storage keeps the lock of one original and the receipts of paid readings. |
| The deputy's answer and its self-check | `deputy.Calls`, with the requests from `deputy.AnswerRequest` and `deputy.SelfcheckRequest`. |

The accounting recovery of an interactive call submits nothing. It settles a verified receipt with `modelcall.SettleReceipt` over the store's own records.

Three flows keep their order in storage and get their model steps from the application. Storage calls each step at its place; it does not build the request and does not see the gateway.

| Flow | Entry in storage | Steps |
|---|---|---|
| The secretary's turn | `Store.DeskStore().Turn` admits the request, places the turn in its conversation, and calls the service once in the transaction of the turn. | `secretary.Steps`: the reading of an attachment, and `Conduct` with the asker and the recall. |
| A workspace command that queues a run | `Store.ExecuteWith` searches memory before it takes the owner lock. | The query embedding. |
| The deputy's run | `Store.RunAgentJobs` and `Store.RunDeputy` hold the leases, the recovery, and the selection of a result. | `deputy.Steps`: the reader and the two calls. The order of the run leaves storage when the deputy's storage is split. That is the last step of the split. |

`internal/app.Application` is the store with these services. The HTTP server, the Telegram channel, and the evaluation tools use it for the secretary, the deputy, a recall, and a workspace command. Compose it after the store has its models.
Tests in the storage package compose the same services in `internal/postgres/compose_test.go`, at each call.

## Storage Packages

The PostgreSQL adapter is being split by domain into storage packages over a shared kernel. The declarations of `internal/postgres` have no cycle among them, so a domain can leave in one behavior-neutral change when the kernel holds what it shares.

| Package | Content | State |
|---|---|---|
| `internal/postgres/kernel` | `DB`, the core of the store: the connection pool, the configured models, the file store, and the business clock. Also the encoding of a document, the checks of a request, the reads of document rows, the lease and the queueing of a job, and the owner lock. No business rule and no table of one domain. | Since 2026-10-12. |
| `internal/postgres` | Everything else, and the composition of the store. `Store` embeds the kernel's `DB`. | Each domain leaves in its own change. The deputy's storage leaves last. |

## Pattern for the Next Paths

1. Put each business rule in the domain package as a function that needs no database.
2. Give the service the smallest storage interface for its operation. Name each method for the business read or write.
3. Keep one atomic business operation in one storage transaction. The adapter opens the transaction and calls the service's function with a unit interface.
4. The service decides. The unit executes. A unit method must not contain a business condition.
5. Construct the service in `internal/app`. Remove the handler method from the storage type.
6. Write owner contract tests against an in-memory implementation of the interfaces.
7. Keep the existing database tests. Call the service where they called the storage method.
8. Add the package to the domain boundary test in `internal/app/boundaries_test.go`.

## Behavior

The path keeps its behavior. The model input has the same bytes as before.
The saved input record keeps its form, so a result that was paid before the release is applied after it.

| Rule | Location |
|---|---|
| A start date counts back whole workdays from the due day. | `effort.StartDate` |
| A task with a start date has one start reminder at the owner's review time. | `effort.PlanTask` |
| A proposal changes only a task that is unchanged since the snapshot and has no estimate. | `effort.Service.Process` |
| A user estimate is never replaced. | `effort.Service.Process` |
| An unreadable output or a task without a proposal returns the job to the queue with a visible reason. | `effort.Service.Process` |

## Temporary Parts

| Part | Callers | Removal condition |
|---|---|---|
| `deputy.Steps` given to `Store.RunAgentJobs` and `Store.RunDeputy` | The deputy's run, whose order is in storage. | The deputy's run leaves the storage package. | The last model call leaves the storage package. The application then builds the gateway from the store's call records. |
| `maintainTaskPlanTx` and the scheduler query `enqueueEffortTx` in `internal/postgres/effort.go` | Item saves and the status scheduler. | The workspace item commands and the scheduler leave the storage package. |
| `workspace.ContentHash` used by storage code | Deputy and secretary input checks. | Those paths leave the storage package. |

## Checks

| Check | Result |
|---|---|
| Owner contracts in `internal/workspace/effort` | Pass without a database, in less than one second. |
| Domain boundary test in `internal/app` | Pass. |
| Phase 3 planning acceptance tests | Pass: user estimate, start dates, start reminder, timeline, paid-result recovery, and stage budgets. |

These acceptance tests ran only with a switch when the pilot was written. The switch is removed, and they run in every complete run.
