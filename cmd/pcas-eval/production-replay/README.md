# Production-Copy Replay Diagnostic

This command invokes listed application operations on a verified disposable production copy.
It does not start the product server, polling, notifications, or a worker loop.
See the [assigned scope](../../../docs/tasks/foundation-recorded-replay.md) for authority and remaining acceptance requirements.

## Commands

```sh
go run ./cmd/pcas-eval/production-replay --manifest /private/case.json
python3 scripts/foundation_replay_compare.py \
  --left-before /private/record/before.jsonl.gz \
  --left-after /private/record/after.jsonl.gz \
  --right-before /private/replay/before.jsonl.gz \
  --right-after /private/replay/after.jsonl.gz \
  --output /private/comparison.json
```

Keep the manifest and all evidence outside Git. Use private directories and files.
The runner refuses a database without its matching container identity, ownership labels, and loopback endpoint.
Its base database is `pcas_replay`. Named clones must use the `pcas_replay_` prefix.
The database bind mount must identify the same private copy root as the manifest.
Writable files, settings, homes, recordings, and output must remain within that root.
Restore files as well as database state before each comparison side.
A new run requires a new output directory and dedicated Codex home.

## Manifest

The JSON manifest uses version `1`. Unknown fields and trailing data fail validation.

| Field | Requirement |
|---|---|
| `case_id`, `revision` | Private case identity and application revision being exercised. |
| `copy_manifest`, `database` | Verified copy manifest and optional owned clone name. |
| `mode` | `record` or `replay`. |
| `models`, `settings`, `blob_dir` | Copied private configuration and files. |
| `codex_home`, `codex_shim` | Dedicated home and `scripts/foundation_model_replay.py`. |
| `real_binary` | Actual Codex executable. Required only for recording; forbidden for replay. |
| `recording` | Completed recording directory. Required only for replay. |
| `call_limit`, `http_call_limit` | Explicit live-call allowances for this diagnostic case. |
| `minimum_calls` | Minimum observed model calls needed for case coverage. |
| `business_time` | Optional fixed RFC 3339 case time. Recording and replay must use the same value. Omit it only for cases that do not require a fixed business clock. |
| `output` | New private evidence directory. |
| `operations` | Ordered list of explicitly selected operations. |

An operation has an `id` and a `kind`.
The kinds `secretary`, `ingest`, and `command` accept their existing application request as `request`.
A `background` operation requires `stage`, `lease_token`, and either `job_id` or `source_from`.
`source_from` names an earlier ingest operation.
For a new ordinary source, select `source.chunk` before `source.extract`.
The fixture cannot lease completed, failed, blocked, or active jobs.
It preserves the normal worker's handler, retry, block, defer, and fencing checks.
It does not prove the general queue's ordering or selection rules.

Allowances count attempted requests. Set each allowance from the selected operations and their existing call policies.
Overflow and capture failure stop additional submissions and leave explicit counters.
The diagnostic allowance does not replace the application's budget policy.
Codex frames and HTTP responses have an 8 MiB capture limit, from their existing adapters.
Overflow fails capture. The diagnostic does not truncate an accepted reply.

## Evidence and Interpretation

Each run stores operation results, model replies, transport counters, and compressed owner-data snapshots.
Snapshots retain every field. Compression does not change their contents.
The report keeps model resources separate from resources used to execute a replay.
It records business time and each operation's actual start time separately.
The fixed clock supplies secretary date interpretation and current-time input, date-tidy judgment, effort input, and topic-project input.
Lease expiry, access checks, retries, timeouts, operational quotas, and accounting use actual time.
Database timestamps and other execution metadata remain actual. This is not a global clock replacement.
Recorded usage is not new replay spending.
An operation failure can be a completely recorded case. Read its error and result before interpreting completion.
The command summary includes `operation_errors`. Receipt states remain in each operation result.
A completed recording does not establish correct product behavior.

Replay checks inputs, model mode, output-schema order, and single consumption before returning a saved reply.
HTTP matching also includes the operation, endpoint, format, and provider-version headers.
Authentication headers are excluded. Offline configuration can use an explicit placeholder to preserve recorded provider availability.
Do not inherit the operator's provider keys.
Missing or mismatched replies cause visible failure. Replay has no live fallback.

The state comparator uses exact row multisets. It preserves decimal precision and all scalar types.
It excludes no fields and does not replace generated identities.
Changed versions, deadlines, relationships, identities, and runtime fields therefore remain findings.
Different starting data cannot pass comparison.
A mismatch is not proof of a business regression; inspect the retained raw data.
The current comparator cannot certify refactors that produce different random identities or runtime metadata.

`scripts/foundation_replay_identity.py` establishes declared identity correspondence for added-row differences.
It requires an exact comparison with equal starting data, complete delta rows, database column types, and the original starting snapshot.
It pins historical UUIDs and rejects ambiguous keys or conflicting mappings.
It maps typed UUID columns and declared entity, memory, receipt, capture, and secretary references.
It does not replace UUID-like text. Every field remains in the resulting comparison.
Its output keeps unresolved keys, unmapped object identities, and runtime differences visible.
It does not certify state equivalence. Missing reservation-to-usage links cannot be inferred from equal amounts.

The first secretary recording exposed a changing business clock in its prompt.
The coordinator approved a business-clock interface. A new recording and replay must verify it.
Do not remove time from matching to make the case pass.

## Synthetic Checks

```sh
go test -race ./cmd/pcas-eval/production-replay
python3 scripts/model_replay_test.py
python3 scripts/replay_state_test.py
```

These checks use fictional protocol responses and private temporary files.
Ordinary CI runs them without real-model credentials or production data.
Private model capture is not a fork-triggered CI job.
