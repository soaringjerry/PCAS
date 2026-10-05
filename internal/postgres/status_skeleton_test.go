package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestStatusSkeletonMigrationDefaultsAndCascades(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, `{}`)
	source := b1Source(t, s, scope, "虚构便签", "虚构人物偏爱清晨读书。", "manual")
	ref := b1Claim(t, s, scope, "虚构人物偏爱清晨读书。", "preference", "adopted", source)
	ctx := context.Background()
	var retired string
	var by *string
	var compared int
	if err := s.pool.QueryRow(ctx, "SELECT retired,retired_by::text,compared FROM claims WHERE owner_id=$1 AND id=$2", scope.OwnerID, ref.ID).Scan(&retired, &by, &compared); err != nil || retired != "" || by != nil || compared != 0 {
		t.Fatal("unexpected retirement defaults", retired, by, compared, err)
	}
	_, err := s.pool.Exec(ctx, "UPDATE claims SET retired='superseded' WHERE owner_id=$1 AND id=$2", scope.OwnerID, ref.ID)
	var check *pgconn.PgError
	if !errors.As(err, &check) || check.Code != "23514" {
		t.Fatal("retirement accepted without target", err)
	}
	for _, purpose := range []string{"vision", "organize", "compare", "card", "handover", "reader", "selfcheck"} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO model_usage(owner_id,id,purpose,model,input_tokens,output_tokens,cost) VALUES($1,$2,$3,'fictional-model',0,0,0)`, scope.OwnerID, memory.NewID(), purpose); err != nil {
			t.Fatal(purpose, err)
		}
	}
	var tiers int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND tier=''", scope.OwnerID).Scan(&tiers); err != nil || tiers != 7 {
		t.Fatal("default tier changed", tiers, err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO status_cards(owner_id,key,kind,name,rule) VALUES($1,'self:taste','self','虚构口味',1)`, scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO status_card_items(owner_id,key,field,position,claim_id,claim_version) VALUES($1,'self:taste','preference',1,$2,$3)`, scope.OwnerID, ref.ID, ref.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,title) VALUES($1,$2,$3,$4,'recurring','虚构固定安排')`, scope.OwnerID, memory.NewID(), ref.ID, ref.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO handovers(owner_id,body,rule) VALUES($1,'',1)`, scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_records WHERE owner_id=$1 AND id=$2", scope.OwnerID, ref.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"status_card_items", "deadlines"} {
		var count int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE owner_id=$1", scope.OwnerID).Scan(&count); err != nil || count != 0 {
			t.Fatal("memory deletion left derived rows", table, count, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
}

func TestStatusSkeletonEmptyOwnerAPI(t *testing.T) {
	s, scope := testStore(t), owner()
	for _, path := range []string{"/v1/workspace/about", "/v1/workspace/about?key=self:rule"} {
		response := b1HTTP(t, s, scope, "GET", path, nil)
		var out workspace.About
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &out) != nil || out.Handover.Body != "" || out.Cards == nil || len(out.Cards) != 0 || out.Deadlines == nil || len(out.Deadlines) != 0 || out.Building != (workspace.BuildingProgress{}) {
			t.Fatal("unexpected empty about", response.Code, response.Body.String())
		}
		delegate := scope
		delegate.IsOwner = false
		if got := b1HTTP(t, s, delegate, "GET", path, nil); got.Code != 403 {
			t.Fatal("about accepted non-owner", got.Code)
		}
	}
	b1Model(t, s, `{}`)
	source := b1Source(t, s, scope, "虚构便签", "虚构人物偏爱蓝色便签。", "manual")
	ref := b1Claim(t, s, scope, "虚构人物偏爱蓝色便签。", "preference", "adopted", source)
	m, err := s.GetMemory(context.Background(), scope, string(ref.ID))
	if err != nil || m.Trust != "stated" || m.Retired != "" || m.RetiredBy != "" || m.MergedFrom != 0 {
		t.Fatal("new fields changed memory behavior", m, err)
	}
	response := b1HTTP(t, s, scope, "GET", "/v1/workspace/memories?retired=1", nil)
	var page workspace.MemoryPage
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Items == nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatal("retired skeleton returned current memories", response.Code, response.Body.String())
	}
	if CompareVersion != 1 || CardVersion != 1 || HandoverVersion != 1 {
		t.Fatal("initial rule versions must be one")
	}
}
