# D1–D3 independent runtime harness freeze

Product used for compilation: `fb1c3eae593527640de156fff806d8789ab5e282`.
The immutable expectations remain `testdata/phase2/delegation-sequences.json`
(SHA256 `c5a122c5058bee0824c799fa0e900f3c2dda4faa0b95b27d663a309054f6c143`).

`TestPhase2RuntimeSourceDerivedDelegationRoleIsolationAndABA` adds three real
Store/HTTP leaves in the independently owned `phase2_runtime_delegation_test.go`.
No product edits, authorization SQL, fake claims, provider-interface stubs, or
dynamic test executions were used to construct this harness.

The fixed owner's query is original E1's question concatenated with the frozen
delegation instruction: `成都预约资料的预约码、集合时间和地点是什么？请让副手整理这份资料的安排`.
It contains no source atom. This choice was made before dynamic execution to
retain the previously established E1 retrieval positive control. The fake
secretary emits the first original source atom in a `delegate:new` title and
prompt; deputy output is exactly `DELEGATE-RESULT-893 Q7-LANTERN-482`, Used=[].
The original zero-claim source body, atoms and input gold are unchanged.

| Leaf | Real control and assertions |
| --- | --- |
| D1 | Secretary actual received bytes and manifest contain original source; delegate is skipped, deputy HTTP count zero, no applicable result or unrestricted title/prompt in current work. |
| D2 | Separate public policies for each role; real queued run and RunAgents; exact deputy bytes, real automatic adoption, no minted claims, exact indirect lineage and durable dependency under deputy policy. Direct/indirect overlap must remain visible. |
| D3 | Same real completion/adoption control, undo auto-adoption before revoke, secretary remains lawful, old run read/export/adoption and diagnostic snapshot stay invalid after revoke and ABA; distinct new run after regrant uses current policy revision. |

Controlled old adoption rejection accepts only Forbidden or stale Conflict:
existing adoption has first been undone. Body clearing, invalidated attempt,
read/export and fresh-generation controls independently distinguish the
authorization cause. Retention independence is covered by the existing separate
runtime retention test; this new leaf does not pretend to perform retention.

Compile command: `go test ./internal/postgres -run '^$' -count=1`.
The initial compile found a test-only field mismatch (`InputRecord` has no
Authorization); the assertion now checks the real returned run's typed dependency
stamp. Compilation passes, with zero tests executed. Dynamic acceptance remains
pending the root-issued combined product/harness SHA. No passing runtime claim
is made here. Other create/update action-field lineage remains separately open.
