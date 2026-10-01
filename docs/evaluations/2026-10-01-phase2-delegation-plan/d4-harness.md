# D4 independent harness freeze

`TestPhase2RuntimeQueuedDelegationRequiresOriginalSecretaryActions` adds two leaves
in the C-owned `phase2_runtime_delegation_test.go`: delegate:new and
create_task→N1→delegate. It implements the separately frozen D4 gold
`e37b2f0` / SHA256 `959375ac05f3a3bd7a450c4ed729ec04dfaefa59f49afbe0d506dcbfab3422ad`.
Original D1–D3 remain unchanged apart from the previously approved exact
ErrUnavailable snapshot protocol fix, whose original red evidence remains.

The root-approved `2bdf253` wire fields are inspected through JSON on real
server-returned values: Run.contextDeskActions, Task/Manifest.desk_actions. The
test does not inject these IDs into a client command or predict final IDs from
the preparation plan. Each required create/delegate ID must come from a real
successful receipt, match the same committed action_log turn, retain original
secretary recipient in context_task, and have durable artifact/actionID/1 exact
source and original secretary policy stamp. Run and Task sets must agree;
completed actual manifest must freeze the same set.

Each leaf first genuinely completes and auto-adopts a deputy result, then undoes
that adoption. A separate second DeskTurn commits another queue before workers
start it. This second turn may lawfully read the first task's fields: inherited
origins are permitted only from the already verified successful first receipts.
This turn's own real create/delegate IDs are mandatory; duplicates, unknown IDs
and speculative IDs fail. True database status must be queued, not inferred
from the user-facing logical status.

Only the original secretary source policy is then revoked. Real authorization
GET proves the deputy's original ID/revision/canonical recipient remains allow,
and trusted deputy GetSource reads unchanged original source. RunAgents must
dispatch zero HTTP requests for the old queue. Current old completed/queued
views must be stale and lack Output/Brief/private generated Prompt/adoption;
the genuinely completed old result (whose adoption was undone) must refuse
new adoption with controlled Forbidden/stale Conflict. Finally, distinct new
independent deputy work actually sends the original source atoms and completes
under the still-valid destination policy. The queue's absent result is never
used as proof of old completed-result invalidation.

Actual fake HTTP receive bytes, manifests, committed receipts, policy response,
old/new run state and origin sets are recorded through the existing evidence
helper. Used remains [], no claims or raw grants are manufactured, no production
edits or observer API stubs are added.

Development compiles against original product `7706baa4f7c66fb7415c294f2bd703df62503154`
with C's approved subsequent fixtures/gold/harness; new server fields are JSON
reads so the earlier typed package can compile. Command
`go test ./internal/postgres -run '^$' -count=1` exits0 with **zero tests run**.
Dynamic D4 acceptance requires root's precise combined head containing A's
final origin implementation and this harness. This document claims no runtime
pass and does not replace D's other action-field matrix or final full PG.
