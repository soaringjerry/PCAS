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

The topic project service holds the proposal rules: a match must name a project that the model saw, a new project waits until the model has seen every project, an existing name attaches instead of creating, the user's own name for a project is kept, and four projects are created in one local day at most.
Its input assembly reads memory and clips evidence in one function with SQL. That function stays in storage until memory retrieval has its own interface.

The date review service holds the proposal rules for a remembered date that is not a time to show.
A task or an idea is created and the date is closed in one recorded action, through the workspace item command and the memory date command.
A kept date is recorded too, so that it is not reviewed again unchanged. A date that changed during the call gets no verdict.

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
| `Store.GenerateForJob` and `Store.HasSavedResult` in `internal/postgres` | The effort service. | Background coordination owns the queue's failure policy and the saved-result interface. |
| `maintainTaskPlanTx` and the scheduler query `enqueueEffortTx` in `internal/postgres/effort.go` | Item saves and the status scheduler. | The workspace item commands and the scheduler leave the storage package. |
| `workspace.ContentHash` used by storage code | Deputy and secretary input checks. | Those paths leave the storage package. |

## Checks

| Check | Result |
|---|---|
| Owner contracts in `internal/workspace/effort` | Pass without a database, in less than one second. |
| Domain boundary test in `internal/app` | Pass. |
| Phase 3 planning acceptance tests | Pass: user estimate, start dates, start reminder, timeline, paid-result recovery, and stage budgets. |

These acceptance tests ran only with a switch when the pilot was written. The switch is removed, and they run in every complete run.
