package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase2Runtime024RecoveryUsesExecutionLeaseAndFixedStartupCutoff(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	ctx := context.Background()
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	type fixture struct {
		name, state, layer, want string
		created, expires         time.Time
		reserved, dispatched     *time.Time
		id                       memory.ID
	}
	old, expired, active := cutoff.Add(-10*time.Minute), cutoff.Add(-5*time.Minute), cutoff.Add(3*time.Minute)
	sent := cutoff.Add(-8 * time.Minute)
	cases := []fixture{
		{name: "prepared_no_send_fact", state: "prepared", want: "outcome_unknown", created: old, expires: expired},
		{name: "reserved_without_observed_dispatch", state: "prepared", want: "outcome_unknown", created: old, expires: expired, reserved: &sent},
		{name: "observed_dispatch_uncompleted", state: "dispatched", want: "outcome_unknown", created: old, expires: expired, reserved: &sent, dispatched: &sent},
		{name: "unexpired_active_execution", state: "prepared", want: "prepared", created: cutoff.Add(-2 * time.Minute), expires: active},
		{name: "created_after_fixed_startup_cutoff", state: "prepared", want: "prepared", created: cutoff.Add(time.Second), expires: cutoff.Add(2 * time.Second)},
		{name: "manual_prepared_no_automatic_lease", state: "prepared", want: "prepared", created: old, layer: "manual_package"},
		{name: "completed_not_recovered", state: "completed", want: "completed", created: old, expires: expired, dispatched: &sent},
		{name: "invalidated_not_resurrected", state: "invalidated", want: "invalidated", created: old, expires: expired},
	}
	for i := range cases {
		c := &cases[i]
		c.id = memory.NewID()
		if c.layer == "" {
			c.layer = "serialized_request"
		}
		var lease *time.Time
		if !c.expires.IsZero() {
			lease = &c.expires
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO context_attempts(owner_id,id,operation_id,ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot_state,input_bytes,observation_layer,dispatch_reserved_at,dispatched_at,created_at,body_expires_at,metadata_expires_at,execution_expires_at) VALUES($1,$2,$3,1,$4,'{}','knowledge','{"version":1,"input":[]}',65536,'capacity_omitted',0,$5,$6,$7,$8,$8::timestamptz+interval '7 days',$8::timestamptz+interval '30 days',$9)`, string(scope.OwnerID), string(c.id), "synthetic-crash-"+c.name, c.state, c.layer, c.reserved, c.dispatched, c.created, lease)
		if err != nil {
			t.Fatal(c.name, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE context_attempts a SET metadata_bytes=octet_length((to_jsonb(a)-'snapshot'-'metadata_bytes')::text) WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	// The explicit clock exercises recovery without waiting or pretending that
	// a canceled HTTP request proves a crashed process has recovered.
	if err := s.RecoverContextAttempts(ctx, cutoff, cutoff.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var state, receipt string
			var dispatched *time.Time
			if err := s.pool.QueryRow(ctx, "SELECT state,dispatched_at,external_receipt FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(c.id)).Scan(&state, &dispatched, &receipt); err != nil {
				t.Fatal(err)
			}
			if state != c.want || receipt != "unknown" {
				t.Errorf("recovery state=%s want=%s receipt=%s", state, c.want, receipt)
			}
			if (dispatched == nil) != (c.dispatched == nil) || dispatched != nil && !dispatched.Equal(*c.dispatched) {
				t.Error("recovery fabricated or lost the prior observed dispatch fact")
			}
			phase2RTMeasuredMetadata(t, s, scope, c.id)
			phase2RTEvidence(t, "recovery-state", map[string]any{"fixture": c.name, "attempt_id": c.id, "state": state, "dispatched_at": dispatched, "receipt": receipt, "startup_cutoff": cutoff, "prior_external_send": "synthetic SQL checkpoint; no actual provider request claimed"})
		})
	}
	// The process cutoff stays fixed on periodic sweeps; an execution created
	// later remains excluded even when its synthetic deadline has now passed.
	if err := s.RecoverContextAttempts(ctx, cutoff, cutoff.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.pool.QueryRow(ctx, "SELECT state FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(cases[4].id)).Scan(&state); err != nil || state != "prepared" {
		t.Error("periodic sweep advanced startup cutoff and recovered a live-process-created attempt", state, err)
	}
	if capture.count() != 0 {
		t.Error("recovery automatically replayed a provider call")
	}
	if err := s.RecoverContextAttempts(ctx, time.Time{}, cutoff); err == nil {
		t.Error("zero startup cutoff accepted")
	}
}

func TestPhase2Runtime024BackfillsOnlyAutomaticExecutionLease(t *testing.T) {
	s := phase2RTLegacy022Store(t)
	phase2RTApplyLegacy023(t, s)
	ctx, scope := context.Background(), owner()
	created := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	ids := map[string]memory.ID{"serialized_request": memory.NewID(), "adapter_arguments": memory.NewID(), "manual_package": memory.NewID()}
	for layer, id := range ids {
		_, err := s.pool.Exec(ctx, `INSERT INTO context_attempts(owner_id,id,operation_id,ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot_state,input_bytes,observation_layer,created_at,body_expires_at,metadata_expires_at) VALUES($1,$2,$3,1,'prepared','{}','knowledge','{"version":1,"input":[]}',65536,'capacity_omitted',0,$4,$5,$5::timestamptz+interval '7 days',$5::timestamptz+interval '30 days')`, string(scope.OwnerID), string(id), "synthetic-old-"+layer, layer, created)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for layer, id := range ids {
		t.Run(layer, func(t *testing.T) {
			var expiry *time.Time
			var state string
			if err := s.pool.QueryRow(ctx, "SELECT execution_expires_at,state FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id)).Scan(&expiry, &state); err != nil {
				t.Fatal(err)
			}
			if layer == "manual_package" && expiry != nil || layer != "manual_package" && (expiry == nil || !expiry.Equal(created.Add(5*time.Minute))) {
				t.Error("migration lost automatic five-minute lease or gave manual an automatic execution deadline")
			}
			if state != "prepared" {
				t.Error("DDL migration changed uncertain transport state")
			}
			phase2RTEvidence(t, "024-upgrade", map[string]any{"layer": layer, "attempt_id": id, "created_at": created, "execution_expires_at": expiry, "state": state})
		})
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("repeat upgraded migration", err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE context_attempts SET execution_expires_at=NULL WHERE owner_id=$1 AND observation_layer='serialized_request'", string(scope.OwnerID)); err == nil {
		t.Error("automatic execution accepted missing lease")
	}
	if _, err := s.pool.Exec(ctx, "UPDATE context_attempts SET execution_expires_at=created_at+interval '5 minutes' WHERE owner_id=$1 AND observation_layer='manual_package'", string(scope.OwnerID)); err == nil {
		t.Error("manual package accepted automatic execution lease")
	}
}
