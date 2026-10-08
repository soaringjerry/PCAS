# PCAS Development Workflow

Current workflow. Updated 2026-10-08.

This workflow applies to development, review, and release.
Product decisions come from the [Whitepaper](../whitepaper.md).
The [Documentation Index](../README.md) separates current specifications from historical material.

## 1 Direction and Scope

Discuss a product or architecture change before assigning its implementation.
Update the whitepaper when the agreed direction changes.
Then prepare the applicable batch scope and complete window prompts.
Use the agreed window names. Send instructions only to windows that have work.

Give each executor the goal, behavior, interfaces, ownership, checks, and delivery conditions.
Let executors select implementation details in that scope.

The coordinator handles integration, small tasks, and product-direction corrections.
Independent batches can proceed in parallel when their responsibilities and shared interfaces are clear.
A historical window assignment does not authorize current work.

Use a separate Git worktree for each concurrent executor.
Before a Git commit, examine the staged diff. Stage only the files assigned to your scope.
Do not switch branches in another executor's worktree.

## 2 State and Dependencies

Before changing state behavior, record the affected operation sequences and expected results.
Use the sequences that apply to the change:

- Consecutive actions and reverse undo.
- Undo after a subsequent action on the same object.
- User or background changes during generation.
- Duplicate requests and replay.
- Concurrent changes to the same object.
- Time boundaries and time-zone changes.
- Actions across web and external channels.
- Source deletion or loss of access.

Examine related rules together for contradictions.
Write product behavior and internal implementation details in different sections.
Keep necessary dependencies explicit.

## 3 Verification

Select checks for the affected behavior and risk.
For significant state changes, a different reviewer must do acceptance checks against the agreed behavior.
Use random operation sequences when their invariants provide useful coverage.
Do not add duplicate checks that only repeat the implementation.
Historical fixtures must not create an unsupported production compatibility requirement.

Record each finding and its reason.
If a finding temporarily skips a test, show the finding in the report.
Remove the skip after repair. Do not report a skipped finding as passed.

Use the actual default model channel for model acceptance. The recorded default is Codex.
Use representative data volume, group sizes, language, time zone, and concurrency.
Examine the generated content and executed action, not only whether an output exists.
When model inputs or output formats change, inspect the real prompt and raw output on an isolated data copy.

When live model behavior fails, collect that evidence before changing the program.
Keep prompts and raw outputs private. Public examples and reports use synthetic data.

## 4 Resource and Failure Rules

For each limit that changes results, give its reason, overflow path, and omitted count.
Keep budgets for background stages explicit.
Use different status labels for deferred, failed, and completed work.
Keep accepted data after a failure. Leave incomplete work incomplete.

If a saved model result cannot be stored, retry storage without a new paid call.
Process only the affected data when possible.
Keep semantic decisions in the model path. Use the program to validate structured results.
Do not write business data through read interfaces.

Keep necessary model-output structure and field order.
Do not assume that all providers accept the same parameters or supply the same capabilities.

## 5 Review and Merge

In the PR, give the final behavior, implementation effect, checks, and known gaps.
Review the affected rules, data paths, and dependencies.
The coordinator can merge an executor's PR when review has no unresolved findings and CI passes.
Real-model quality and live deployment status must have evidence in addition to passing CI.

## 6 Release

Release authorized work after merge and passing CI. No additional routine release confirmation is necessary.

1. Confirm the approved revision and deployment target.
2. Back up the database with `pg_dump`.
3. Make sure that the backup completed successfully.
4. Keep necessary file and private configuration backups.
5. Apply migrations.
6. Deploy the approved revision.
7. Examine readiness, logs, and the affected functions.
8. Test the live secretary through the real default Codex channel.
9. Examine each action, receipt, and error.
10. Clean only the test records identified by request or source ID.
11. Examine claim counts and related test sources after cleanup.
12. Record the result and any remaining failure.

Show errors. Do not replace product behavior with manually written production content.
The check-only `smokeId` path has limited capabilities; see [Deployment](../deployment.md#7-live-checks).
A skipped memory action has no execution result.

Claims can change during background work. Attribute count changes to their source or request.
Do not select production cleanup records by creation time.

## 7 Test Environment

Use isolated test databases and your own test services.
Stop a test process by its PID. Do not use host-wide `pkill` by process name.
Run Playwright and Chromium with `env -u DISPLAY`.
Keep production credentials and data out of public test artifacts.

## 8 Documentation and History

Write current specifications in English with the [Writing Guide](../writing-guide.md).
Write code and commit messages in English. Communicate with the user in Chinese.
List each repository document in the documentation index.
Update links and status in the same change as a merge, move, or retirement.

Keep one authoritative location for each rule.
Link to that rule instead of copying it into each batch.
Use different status labels for product targets, implemented functions, deployment records, and open findings.
Keep historical conclusions. Mark superseded material so it cannot govern new work.

Do not restore the rolled-back Phase 2.0 design.

## 9 Maintenance

Find the cause of a defect: implementation, conflicting rules, or a structural dependency.
When replacing a function, identify its previous entry points, configuration, prompts, and tests.
Remove obsolete parts when their real use and upgrade dependencies permit removal.
For compatibility, identify the supported data or caller and the removal condition.

Keep query fields in storage. Apply scope filters during retrieval.
Finish pending writes before closing storage.
Keep generated binaries and private indexes out of version control.
