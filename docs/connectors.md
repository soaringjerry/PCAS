# PCAS Input Reference

Implementation reference. Updated 2026-10-08.

Generic inputs include webhooks, HTTP polling, folders, and conversation archives.
Each record keeps a source identity, version, text, statement time, role, parent message, and branch when supplied.
Duplicate source versions do not create duplicate memory.
Source storage and asynchronous interpretation have different progress states.
Native application adapters are additional delivery work; see [Project Status](status.md).

## 1 Webhook Input

Create the connector in settings. The interface supplies its write-only Bearer token once.
A paused connector rejects writes. The token cannot read memory or manage the workspace.

```http
POST /v1/connectors/{id}/records
Authorization: Bearer CONNECTOR_TOKEN
Content-Type: application/json
```

```json
{
  "records": [{
    "id": "conversation-42/message-7",
    "version": "3",
    "title": "Project discussion",
    "text": "I decided to use the shared memory service.",
    "media_type": "text/plain",
    "expressed_at": "2026-09-29T09:00:00Z",
    "conversation_id": "conversation-42",
    "parent_id": "message-6",
    "role": "user",
    "branch": "active",
    "episode_key": "project-example",
    "episode_title": "Project discussion",
    "missing_attachments": []
  }],
  "gaps": []
}
```

The record must have an ID and nonempty text.
An omitted version uses a stable content hash. Content must not change under an explicit version.
An explicit `episode_key` can connect a shared episode across inputs.
Without that key, equal conversation names do not identify a shared episode.

The response reports references, imports, duplicates, blocked records, and gaps.
Blocked records do not prevent other accepted records from import.

## 2 HTTP Polling

The data source returns the record format above with pagination fields:

```json
{"records": [], "next_cursor": "page-2", "has_more": true, "gaps": []}
```

Later requests include `?cursor=...`.
A continued page must have a new nonempty cursor.
The service advances the cursor after successful data commit.
The final ETag is an input to subsequent conditional requests.
Failures keep the cursor and expose their state.

The source must use incremental cursors to prevent repeated retrieval of previous pages.

Private source credentials use a configured environment variable with the `PCAS_CONNECTOR_` prefix.
Credentials stay on the server. Redirects must not forward them to a different destination.
See [Connector Processing](../internal/postgres/connectors.go).

## 3 Folder Input

The native path is `PCAS_INBOX_DIR/{connector ID}`.
Compose uses `/var/lib/pcas/inbox/{connector ID}` in the persistent file volume.
Supported formats include TXT, Markdown, JSON, JSONL, and ZIP.
The service scans regular files in that folder. It does not follow symlinks or recurse into subfolders.

Content fingerprints identify new or changed files.
Removing an upstream file does not delete stored memory.
The connector does not move or delete the input file.

## 4 Conversation Archives

Supported inputs include ChatGPT conversation trees, Claude message archives, generic records, JSONL, TXT, and Markdown.
Preview shows accepted content and gaps before import.
Import batches support pause, resume, and the implemented organization options.
The current web path supports upload pieces for large archives.

```http
POST /v1/connectors/archive/preview
POST /v1/connectors/archive
POST /v1/connectors/archive/uploads
GET /v1/connectors/imports
POST /v1/connectors/imports/{id}/pause
POST /v1/connectors/imports/{id}/resume
```

[Archive Upload Routes](../internal/httpapi/archive_uploads.go) gives piece and completion operations.
[Archive Routes](../internal/httpapi/connectors.go) gives preview, import, and batch operations.

Speaker roles and active or historical branches stay distinct.
ZIP handling selects supported conversation content and does not extract archive paths into the filesystem.
Media that is not parsed stays a gap. Archive import does not include OCR or transcription of each embedded file.

Deleting a source can remove a saved source original of an archive that contains its text.
Other message sources stay stored with their own source identities.
Reimport checks deletion markers and must not reverse a user correction.

## 5 Implemented Limits

These values come from current code paths. Measure capacity and speed through the complete system.

| Input | Value | Overflow behavior | Code |
|---|---|---|---|
| Ordinary attachment | 20 MiB | Reject the oversized binary. | [File store](../internal/blob/files.go). |
| Archive upload | 512 MiB | Return an archive-size error. | [Archive reader](../internal/connectors/archive_stream.go). |
| Decoded archive content | 512 MiB | Return an archive-size error. | [Archive reader](../internal/connectors/archive_stream.go). |
| Kept archive messages | 200,000 | Keep newer entries and report `LeftOut`. | [Archive reader](../internal/connectors/archive_stream.go). |
| Upload piece | 8 MiB | Reject an oversized piece. | [Upload routes](../internal/httpapi/archive_uploads.go). |
| Connector record batch | 100 records | Reject an oversized batch. | [Connector write](../internal/postgres/connectors.go). |
| One record's text | 1 MiB | Reject invalid content. | [Record validation](../internal/connectors/archive.go). |
| Parsed PDF | 100 pages | Keep the source and report a parsing failure. | [Attachment processing](../internal/postgres/attachments.go). |

The former 20 MiB archive and 10,000-message values are obsolete.
Ordinary attachment limits still apply to ordinary attachment uploads.
Large-archive selection is bounded. `LeftOut` means incomplete import, not complete coverage.
This reference does not establish a measured capacity rationale for each limit.
For new or changed limits, record the reason, overflow path, and count in the applicable change.

## 6 Derived Summaries and Activity

`POST /v1/memory/summary` returns an extractive summary with dependencies, coverage gaps, and cache state.
The cache depends on the principal, root version, budget, and members.
Corrections, deletion, access changes, or new members can invalidate it.
A summary is not an independent fact store.

`POST /v1/memory/activity` changes the applicable decay, reinforcement, and pin settings.
User adoption can strengthen activity. Retrieval and display do not count as adoption.
Reminder state, memory validity, and task status are different properties.
