package fixture

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type SeedStore interface {
	Snapshot(context.Context, memory.Scope) (workspace.State, error)
}
type Seeded struct {
	Scope                  memory.Scope
	Sources                map[string]memory.Ref
	NativeStructuredSchema bool
	Claims                 map[string][]memory.Ref
}

func encoded(v any) []byte { b, _ := json.Marshal(v); return b }

// PrepareGoldSchema only supplies frozen contract fields in this evaluator's
// isolated schema before S0 exists. It never creates a production migration.
func PrepareGoldSchema(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var native bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='claim_revisions' AND column_name='event_from') AND to_regclass(current_schema()||'.claim_mentions') IS NOT NULL`).Scan(&native)
	if err != nil || native {
		return native, err
	}
	_, err = pool.Exec(ctx, `ALTER TABLE claim_revisions ADD COLUMN IF NOT EXISTS event_from timestamptz, ADD COLUMN IF NOT EXISTS event_to timestamptz, ADD COLUMN IF NOT EXISTS event_precision text NOT NULL DEFAULT 'unknown';
 CREATE TABLE IF NOT EXISTS claim_mentions (
 owner_id uuid NOT NULL, claim_id uuid NOT NULL, claim_version integer NOT NULL, entity_id uuid NOT NULL,
 role text NOT NULL CHECK(role IN ('person','place','organization','thing')),
 PRIMARY KEY(owner_id,claim_id,claim_version,entity_id,role),
 FOREIGN KEY(owner_id,claim_id,claim_version) REFERENCES claim_revisions ON DELETE CASCADE,
 FOREIGN KEY(owner_id,entity_id) REFERENCES entities ON DELETE CASCADE);
 CREATE INDEX IF NOT EXISTS eval_mentions_lookup ON claim_mentions(owner_id,entity_id,role);`)
	return false, err
}
func Seed(ctx context.Context, store SeedStore, pool *pgxpool.Pool, c Corpus, anchor time.Time, agent string, gold bool) (Seeded, error) {
	out := Seeded{Scope: memory.Scope{OwnerID: memory.NewID(), PrincipalID: "owner", IsOwner: true}, Sources: map[string]memory.Ref{}, Claims: map[string][]memory.Ref{}}
	if _, err := store.Snapshot(ctx, out.Scope); err != nil {
		return out, err
	}
	owner := string(out.Scope.OwnerID)
	if _, err := pool.Exec(ctx, `UPDATE workspace_owners SET settings=settings||jsonb_build_object('timezone',$2::text,'dailyBudget',1000000,'wakeIdeas',false) WHERE owner_id=$1`, owner, c.Timezone); err != nil {
		return out, err
	}
	if _, err := pool.Exec(ctx, `UPDATE workspace_agents SET document=document||'{"includeInferred":true}'::jsonb WHERE owner_id=$1 AND id=$2`, owner, agent); err != nil {
		return out, err
	}
	var err error
	if gold {
		out.NativeStructuredSchema, err = PrepareGoldSchema(ctx, pool)
		if err != nil {
			return out, err
		}
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return out, err
	}
	anchor = anchor.In(loc)
	// Same fixed fixture source insertion order for all methods, fixed by fixture,
	// rather than relevance or the observed implementation output.
	for _, d := range c.Documents() {
		at := anchor.AddDate(0, 0, d.ExpressedDays).Add(9 * time.Hour)
		source := memory.Ref{ID: memory.ID(stableID("source", d.ID)), Kind: memory.SourceKind, Version: 1}
		out.Sources[d.ID] = source
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			recorded := anchor.AddDate(0, 0, d.RecordedDays)
			if err := record(ctx, tx, owner, string(source.ID), "source", at, recorded); err != nil {
				return err
			}
			actor := "user"
			if d.Connector == "archive" {
				actor = "import"
			}
			if _, err := tx.Exec(ctx, `UPDATE record_versions SET actor=$3 WHERE owner_id=$1 AND record_id=$2`, owner, string(source.ID), actor); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,$3,$4)`, owner, string(source.ID), d.Connector, "eval/"+d.ID); err != nil {
				return err
			}
			hash := sha256.Sum256([]byte(d.Text))
			if _, err := tx.Exec(ctx, `INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) VALUES($1,$2,1,'1',$3,$4,$5,'text/plain')`, owner, string(source.ID), hash[:], d.Title, d.Text); err != nil {
				return err
			}
			// The product's exported rune chunker, with its canonical 1200/160 shape.
			for _, chunk := range memory.SplitText(d.Text, 1200, 160) {
				id := stableID("chunk", d.ID+"/"+strconv.Itoa(chunk.Ordinal))
				if err := record(ctx, tx, owner, id, "chunk", at, recorded); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO chunks(owner_id,id,version,source_id,source_version,ordinal,start_rune,end_rune,body,search_text) VALUES($1,$2,1,$3,1,$4,$5,$6,$7,$8)`, owner, id, string(source.ID), chunk.Ordinal, chunk.StartRune, chunk.EndRune, chunk.Text, strings.Join(memory.SearchTokens(chunk.Text), " ")); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) VALUES($1,$2,1,$3,$4,'current') ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET role=excluded.role`, owner, string(source.ID), "eval/"+d.ID, d.Role); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, owner, string(source.ID), agent); err != nil {
				return err
			}
			if err := index(ctx, tx, owner, string(source.ID), d.Title+"\n"+d.Text); err != nil {
				return err
			}
			if !gold {
				return nil
			}
			for itemIndex, item := range d.Gold.Items {
				if err := seedClaim(ctx, tx, out.Scope, source, d, item, itemIndex, at, recorded, agent, loc); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return out, fmt.Errorf("seed %s: %w", d.ID, err)
		}
	}
	if gold {
		// Record gold claim identities via evidence, independent of product output.
		for _, d := range c.Documents() {
			source := out.Sources[d.ID]
			rows, err := pool.Query(ctx, `SELECT c.claim_id::text,c.value#>>'{}' FROM evidence e JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(e.owner_id,e.target_id,e.target_version) WHERE e.owner_id=$1 AND e.source_id=$2`, owner, string(source.ID))
			if err != nil {
				return out, err
			}
			refs := make([]memory.Ref, len(d.Gold.Items))
			for rows.Next() {
				var id, text string
				if err := rows.Scan(&id, &text); err != nil {
					rows.Close()
					return out, err
				}
				for n, i := range d.Gold.Items {
					if i.Text == text {
						refs[n] = memory.Ref{ID: memory.ID(id), Kind: memory.ClaimKind, Version: 1}
					}
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return out, err
			}
			out.Claims[d.ID] = refs
		}
	}
	return out, nil
}
func index(ctx context.Context, tx pgx.Tx, owner, id, text string) error {
	_, err := tx.Exec(ctx, `INSERT INTO record_search(owner_id,record_id,record_version,search_text) VALUES($1,$2,1,$3) ON CONFLICT DO NOTHING`, owner, id, strings.Join(memory.SearchTokens(text), " "))
	return err
}
func record(ctx context.Context, tx pgx.Tx, owner, id, kind string, expressed, recorded time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)`, owner, id, kind); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO record_versions(owner_id,record_id,version,actor,expressed_at,recorded_at) VALUES($1,$2,1,'ai',$3,$4)`, owner, id, expressed, recorded)
	return err
}
func entity(ctx context.Context, tx pgx.Tx, owner, name, kind string, at time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT e.entity_id::text FROM entity_versions e JOIN aliases a ON (a.owner_id,a.entity_id,a.entity_version)=(e.owner_id,e.entity_id,e.version) WHERE e.owner_id=$1 AND e.entity_type=$2 AND lower(btrim(a.alias))=lower(btrim($3)) ORDER BY e.entity_id LIMIT 1`, owner, kind, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	id = stableID("entity", kind+"/"+strings.ToLower(strings.TrimSpace(name)))
	if err := record(ctx, tx, owner, id, "entity", at, at); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entities(owner_id,id) VALUES($1,$2)`, owner, id); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,$3,$4)`, owner, id, kind, name); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)`, owner, id, name)
	return id, err
}
func seedClaim(ctx context.Context, tx pgx.Tx, scope memory.Scope, source memory.Ref, d Document, i Item, itemIndex int, at, recorded time.Time, agent string, loc *time.Location) error {
	owner := string(scope.OwnerID)
	subjectKind := "person"
	if i.Subject == "我" {
		subjectKind = "self"
	}
	subject, err := entity(ctx, tx, owner, i.Subject, subjectKind, recorded)
	if err != nil {
		return err
	}
	id := stableID("claim", d.ID+"/"+strconv.Itoa(itemIndex))
	if err := record(ctx, tx, owner, id, "claim", at, recorded); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO claims(owner_id,id) VALUES($1,$2)`, owner, id); err != nil {
		return err
	}
	confirmation := "adopted"
	if d.Connector == "archive" || i.Acquisition != "direct" || i.Qualification != "asserted" {
		confirmation = "candidate"
	}
	var from, to any
	precision := "unknown"
	if i.When != nil {
		f, err := time.ParseInLocation("2006-01-02", i.When.From, loc)
		if err != nil {
			return err
		}
		t, err := time.ParseInLocation("2006-01-02", i.When.To, loc)
		if err != nil {
			return err
		}
		from, to, precision = f, t, i.When.Precision
	}
	if _, err := tx.Exec(ctx, `INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type,event_from,event_to,event_precision) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,'initial',$9,$10,$11)`, owner, id, subject, i.Predicate, encoded(i.Text), i.Nature, i.Acquisition, confirmation, from, to, precision); err != nil {
		return err
	}
	for _, group := range []struct {
		kind  string
		names []string
	}{{"person", i.People}, {"place", i.Places}, {"organization", i.Organizations}} {
		for _, name := range group.names {
			eid, err := entity(ctx, tx, owner, name, group.kind, recorded)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,$4) ON CONFLICT DO NOTHING`, owner, id, eid, group.kind); err != nil {
				return err
			}
		}
	}
	pos := strings.Index(d.Text, i.Quote)
	start := len([]rune(d.Text[:pos]))
	end := start + len([]rune(i.Quote))
	if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,1,$4,1,$5,$6,'supports')`, owner, string(memory.NewID()), string(source.ID), id, encoded(map[string]int{"start_rune": start, "end_rune": end}), i.Acquisition); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)`, owner, id, agent); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO activity(owner_id,record_id) VALUES($1,$2)`, owner, id); err != nil {
		return err
	}
	return index(ctx, tx, owner, id, i.Text)
}

// Received verifies corpus identities against the dependencies persisted for
// the actual secretary request. Text copied from a different, identical source
// cannot satisfy the requested source/claim identity by accident.
func Received(ctx context.Context, pool *pgxpool.Pool, seeded Seeded, request string) (map[string]bool, error) {
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND request_id=$2`, string(seeded.Scope.OwnerID), request).Scan(&raw); err != nil {
		return nil, err
	}
	var refs []memory.Ref
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, err
	}
	ids := map[memory.Ref]bool{}
	for _, r := range refs {
		ids[r] = true
	}
	received := map[string]bool{}
	for id, source := range seeded.Sources {
		if ids[source] {
			received[id] = true
		}
		for n, claim := range seeded.Claims[id] {
			if ids[claim] {
				received[id] = true
			}
			received[fmt.Sprintf("%s/%d", id, n)] = ids[source] || ids[claim]
		}
		if _, ok := seeded.Claims[id]; !ok && ids[source] {
			received[id+"/0"] = true
		}
	}
	return received, nil
}

func stableID(kind, key string) string {
	sum := sha256.Sum256([]byte("pcas-phase2-eval-v1/" + kind + "/" + key))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
