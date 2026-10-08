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
| `cmd/pcas-eval` | Evaluation tools. Production does not use them. |
| `internal/postgres` | Storage. It also contains many business operations, prompts, and model calls. |
| `internal/postgres/migrations` | Database migrations. |
| `internal/memory`, `internal/workspace` | Memory and workspace types. |
| `internal/ai` | Model provider adapters. |
| `internal/httpapi` | HTTP interfaces. |
| `internal/telegram`, `internal/notify`, `internal/connectors` | Telegram, notifications, and connectors. |
| `internal/worker` | Background job coordination. |
| `web/` | Frontend. |

The business operations in `internal/postgres` are a known structural defect.
Section 10 gives the target structure.

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

## 6 Freeze Until the Foundation Phase

The foundation phase builds the structure in section 10.
Until the coordinator closes that phase, do not increase the items below.
If a task needs one of them, stop and ask the coordinator.

1. Model call paths. Call models only through an existing wrapper, such as `useModelCall`.
   Do not add a wrapper. Do not call `s.models` directly.
2. Prompt forms. Write a new prompt as one top-level constant named `xxxInstructions`.
   Do not assemble prompt instructions with `WriteString` or `Fprintln` inside a workflow.
   Do not join one prompt constant to another prompt constant.
3. Activity, log, and usage tables. Use `action_log`, `activity`, or `model_usage` when possible.
   Do not create a new table for this purpose.
4. New domains in `internal/postgres`. You can change files in an existing domain.
   Do not start a new group of files for a new business concept.

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
Start each message with `feat:`, `fix:`, `docs:`, or `test:`.
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
- The task needs an item that section 6 freezes.
- The task changes model input or an output schema. Workflow section 3 requires a real-model check first.
- The task needs a database migration.
- The task deletes or overwrites data, and undo cannot restore it.
- Two repairs of the same defect failed. Collect the real prompt and raw output before a third attempt.

In other conditions, complete the task scope. Do not stop for routine choices.

## 10 Target Structure

This structure does not exist yet.
Do not make these packages before the coordinator assigns the skeleton task.

| Part | Target |
|---|---|
| `internal/prompts` | All prompts, one file each, embedded with `go:embed`, each with a content hash. Shared rules have one copy. |
| Model gateway | One call path. It records the function, prompt name and hash, memory and sources used, model, model tokens, cost, and duration. |
| Activity stream | One query answers what PCAS did, its cost, and its reason. |
| Domain packages | Business operations move out of `internal/postgres`. Storage only reads and writes data. |

The architecture document will give the complete plan.
