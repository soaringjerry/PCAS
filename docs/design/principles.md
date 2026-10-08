# PCAS Interface Principles

Current interface specification. Updated 2026-10-08.

The interface helps the user get work done and see the result.
The [Whitepaper](../whitepaper.md) defines product behavior and action boundaries.
This document defines presentation. It does not change those boundaries.

## 1 Conversation and Results

Use one secretary input on the home page and in each workspace.
Keep the input available while an answer or background action is in progress.
Preserve an unsent draft after refresh.
Show receipts beside the resulting work.
Describe the actual outcome, including skipped and failed actions.
Ask only about the field that contains a meaningful ambiguity.

## 2 Visual Information

Use cards for sourced answers, timelines for work, and charts for cost.
Use date blocks and a current-time line for schedules.
Use short text for receipts, reasons, and necessary explanations.
Do not repeat a displayed list in a long paragraph.
Give each source a way to open its original content.

The main action controls are completion, change or undo, and required confirmation.
Users can edit a title, note, or document in place.
Show whether an edit is saved. Preserve the draft when saving fails.

## 3 Home

Place the most relevant current need above the secretary input.
Show recent team activity below the input.
Organize remaining information into timed work, tasks, projects, and ideas.
Remove empty sections from the daily view.

Put only applicable timed entries on the timeline.
Keep unresolved dates and arrangements without a usable time outside the timeline.
Show overdue work without making every old entry an urgent warning.
Show automatic work with its source and undo.
Group older entries when necessary. Keep the complete list accessible.

The current layout direction replaces the earlier three-column design.
[Project Status](../status.md) separates the layout direction from delivery.

## 4 Workspace

Show the conclusion, blockers, and next action at the top.
Show the handover update time and sources.
During regeneration, identify the previous handover as the previous result.
If no handover exists, state that it is unavailable.
Correct underlying information through the secretary.

Keep the plan timeline, files, document versions, and deputy results in the workspace.
Show additions and removals when comparing versions.
Show processing and upload failures beside the file.
Keep the secretary input in the same workspace.

## 5 Observation Panel

Put internal processing information in the observation panel.
Show calls, source use, cost, access settings, background stages, and recovery actions there.
Separate deferral from failure in the displayed status.
Allow the user to trace a result back to its action and source.
Daily work must not require approval of each memory or processing step.
Some controls remain in the current library and settings.
These controls do not establish delivery of the complete observation panel.

## 6 Errors and Motion

Explain the failed step, reason, and available recovery action.
Do not show an expected future action as completed work.
Use empty-state text that agrees with the data.
Limit motion to useful changes, such as new receipts and the current-time line.
Stop nonessential motion during input, pointer interaction, or reduced-motion mode.
Keep navigation compact. Avoid duplicate project lists and persistent internal-status controls.
