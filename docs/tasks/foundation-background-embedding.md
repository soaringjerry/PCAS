# Foundation Background Embedding Gateway

Revised by the coordinator on 2026-10-10. This page replaces the earlier scope.
The earlier partial implementation stayed in Sol's worktree. It did not compile and is not on this branch.

This page covers the embedding that `ProcessEmbedding` requests for sources and claims.
The [query embedding page](foundation-query-embedding.md) gives the shared gateway entry and its records.

## Why the Earlier Scope Was Replaced

The earlier scope planned an immutable plan, saved batch results, and a separate recovery chain for each batch.
Its reason was a controlled case: the final vector write failed and the retried job submitted its batches again.
That case used a fake provider. It was not a measurement of production spending.

The existing queue rule already limits this case.
A job with a paid reservation is not submitted again after a failed attempt.
A job with no cost uses the queue's retry, and its repeated requests cost nothing.
The handler also skips every record that already has a vector for the selected model.
The earlier scope therefore added a recovery mechanism for a cost that the existing rules do not permit.

## Behavior

`ProcessEmbedding` keeps its input selection, its estimate, its final version check, and its atomic vector write.
It requests the vectors through the gateway entry `CallEmbedding` with the leased job.

| Step | Record |
|---|---|
| Before the provider requests | One transaction applies the existing job budget rules, reserves the estimate, and inserts a `model_calls` row. |
| Provider requests | The gateway submits the texts in order, at most 32 texts in each request. |
| After the requests | One transaction records the total usage with the job and its memory references, settles the reservation, and stores the outcome. |

A call for a large source can contain several provider requests. Its journal row records their total usage.
The embedding call is its own execution. Its root and cause identify the paying job.
The journal row holds memory references and sizes. It does not hold text or vectors.

| Result | Job outcome |
|---|---|
| Vectors returned | The handler writes them and acknowledges the job in one transaction. |
| Provider not usable before the first request | No call and no reservation. The job retries with `provider_unavailable`. |
| Provider rejects a request | The call is `failed`. A paid job stops with `model_call_failed`. A job with no cost retries. |
| Outcome not known | The call is `unknown` and its reservation stays held. The job follows the same rule as a rejected request. |
| Accounting write fails | The handler returns the error and does not write the vectors. |

## Known Limits

- A failed final vector write loses the returned vectors. A paid job does not request them again automatically.
  The embedding backfill finds the records that have no vector.
- Usage is one row for each call. Before this change it was one row for each provider request.

## Checks

| Check | Result |
|---|---|
| Gateway contracts, including ordered requests for more than 32 texts | Pass. |
| Existing embedding handler tests in `internal/postgres` | See the release record for this revision. |
| Provider boundary test | Pass. The `ProcessEmbedding` exception is removed. |
