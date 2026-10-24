package intakestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// eraseUnit is the storage of one erasure.
type eraseUnit struct {
	store *Intake
	tx    pgx.Tx
	scope memory.Scope
}

func (u eraseUnit) owner() string { return string(u.scope.OwnerID) }

func (u eraseUnit) strings(ctx context.Context, sql string, ids []string) ([]string, error) {
	rows, err := u.tx.Query(ctx, sql, u.owner(), ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (u eraseUnit) Lock(ctx context.Context, id string) (int, error) {
	var version int
	err := u.tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 FOR UPDATE", u.owner(), id).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, memory.ErrNotFound
	}
	return version, err
}

func (u eraseUnit) Sources(ctx context.Context, ids []string) ([]string, error) {
	return u.strings(ctx, "SELECT DISTINCT source_id::text FROM evidence WHERE owner_id=$1 AND target_id=ANY($2::uuid[])", ids)
}

const closureLinks = `SELECT source_id AS parent,id AS child FROM chunks WHERE owner_id=$1 UNION SELECT source_id,target_id FROM evidence WHERE owner_id=$1
		UNION SELECT dependency_id,view_id FROM derived_dependencies WHERE owner_id=$1 UNION SELECT from_id,id FROM relations WHERE owner_id=$1 UNION SELECT to_id,id FROM relations WHERE owner_id=$1
		UNION SELECT subject_id,claim_id FROM claim_revisions WHERE owner_id=$1
 UNION SELECT c.id,s.id FROM claims c JOIN sources s ON s.owner_id=c.owner_id AND s.connector IN ('corrections','memory-input') AND s.external_id=c.id::text WHERE c.owner_id=$1
 UNION SELECT (disambiguation->>'source_id')::uuid,entity_id FROM entity_versions WHERE owner_id=$1 AND disambiguation ? 'source_id'
 UNION SELECT archive_id,source_id FROM archive_entries WHERE owner_id=$1
 UNION SELECT derived_from_id,source_id FROM source_versions WHERE owner_id=$1 AND derived_from_id IS NOT NULL`

const answerLinks = `
 UNION SELECT d.memory_id,s.id FROM run_dependencies d JOIN adopted_artifacts a ON (a.owner_id,a.run_id)=(d.owner_id,d.run_id) JOIN sources s ON s.owner_id=a.owner_id AND s.connector='actions' AND s.external_id=a.thing_id::text WHERE d.owner_id=$1`

func (u eraseUnit) Closure(ctx context.Context, ids []string, answers bool) ([]string, error) {
	links := closureLinks
	if answers {
		links += answerLinks
	}
	return u.strings(ctx, `WITH RECURSIVE affected(id) AS (
		SELECT unnest($2::uuid[]) UNION SELECT links.child FROM affected a JOIN (`+links+`
		) links ON links.parent=a.id) SELECT id::text FROM affected`, ids)
}

func (u eraseUnit) WholeEpisodes(ctx context.Context, ids []string) ([]string, error) {
	return u.strings(ctx, `SELECT DISTINCT e.id::text FROM episodes e WHERE e.owner_id=$1 AND EXISTS(SELECT 1 FROM episode_members m WHERE m.owner_id=e.owner_id AND m.episode_id=e.id AND m.member_id=ANY($2::uuid[])) AND NOT EXISTS(SELECT 1 FROM episode_members m WHERE m.owner_id=e.owner_id AND m.episode_id=e.id AND NOT(m.member_id=ANY($2::uuid[])))`, ids)
}

func (u eraseUnit) RedactArchives(ctx context.Context, ids []string) error {
	archives, err := u.strings(ctx, "SELECT DISTINCT archive_id::text FROM archive_entries WHERE owner_id=$1 AND source_id=ANY($2::uuid[])", ids)
	if err != nil {
		return err
	}
	return redactArchivesTx(ctx, u.tx, u.scope, archives)
}

func (u eraseUnit) LoneSubjects(ctx context.Context, ids []string) ([]string, error) {
	return u.strings(ctx, `WITH candidates(entity_id) AS (
        SELECT subject_id FROM claim_revisions WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])
        UNION SELECT entity_id FROM claim_mentions WHERE owner_id=$1 AND claim_id=ANY($2::uuid[]))
        SELECT DISTINCT e.id::text FROM candidates x JOIN memory_records e ON e.owner_id=$1 AND e.id=x.entity_id
        JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(e.owner_id,e.id,e.version)
        JOIN record_versions created ON(created.owner_id,created.record_id,created.version)=(e.owner_id,e.id,1)
        WHERE ev.entity_type<>'self' AND (created.actor='ai' OR EXISTS(
            SELECT 1 FROM claim_revisions erased WHERE erased.owner_id=e.owner_id AND erased.subject_id=e.id AND erased.claim_id=ANY($2::uuid[])))
        AND NOT EXISTS(SELECT 1 FROM claim_revisions kept WHERE kept.owner_id=e.owner_id AND kept.subject_id=e.id AND NOT(kept.claim_id=ANY($2::uuid[])))
        AND NOT EXISTS(SELECT 1 FROM claim_mentions kept WHERE kept.owner_id=e.owner_id AND kept.entity_id=e.id AND NOT(kept.claim_id=ANY($2::uuid[])))`, ids)
}

func (u eraseUnit) KeepAnswers(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := sourcestore.InvalidateTx(ctx, u.tx, u.scope, id); err != nil {
			return err
		}
	}
	if _, err := u.tx.Exec(ctx, `UPDATE agent_runs SET document=jsonb_set(jsonb_set(document,'{staleContext}','true'),'{outdated}','true')
            WHERE owner_id=$1 AND id IN(SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`, u.owner(), ids); err != nil {
		return err
	}
	if err := retainDeletedMemoryOriginsTx(ctx, u.tx, u.scope.OwnerID, ids); err != nil {
		return err
	}
	// JSON context references remain in the retained result for stale checks;
	// remove relational references before deleting their referenced versions.
	_, err := u.tx.Exec(ctx, `DELETE FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[])`, u.owner(), ids)
	return err
}

func (u eraseUnit) PurgeArtifacts(ctx context.Context, ids []string) ([]string, error) {
	return workstore.PurgeArtifactsTx(ctx, u.tx, u.scope, ids)
}

func (u eraseUnit) Block(ctx context.Context, ids []string) error {
	rows, err := u.tx.Query(ctx, "SELECT connector,external_id FROM sources WHERE owner_id=$1 AND id=ANY($2::uuid[])", u.owner(), ids)
	if err != nil {
		return err
	}
	sources, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]byte, error) {
		var connector, external string
		err := row.Scan(&connector, &external)
		return sourcestore.SourceKey(connector, external), err
	})
	if err != nil {
		return err
	}
	for _, key := range sources {
		if _, err := u.tx.Exec(ctx, "INSERT INTO reimport_blocks(owner_id,source_key_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", u.owner(), key); err != nil {
			return err
		}
	}
	claims, err := u.strings(ctx, "SELECT DISTINCT claim_key FROM claim_source_keys WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])", ids)
	if err != nil {
		return err
	}
	for _, key := range claims {
		value, err := hex.DecodeString(key)
		if err != nil {
			return err
		}
		if _, err := u.tx.Exec(ctx, "INSERT INTO claim_reimport_blocks(owner_id,key_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", u.owner(), value); err != nil {
			return err
		}
	}
	for _, id := range ids {
		hash := sha256.Sum256([]byte(id))
		if _, err := u.tx.Exec(ctx, "INSERT INTO record_reimport_blocks(owner_id,id_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", u.owner(), hash[:]); err != nil {
			return err
		}
	}
	return nil
}

func (u eraseUnit) ScrubCalls(ctx context.Context, ids []string) error {
	return (callstore.InteractiveCalls{Store: u.store.Calls}).ScrubInputsTx(ctx, u.tx, u.scope.OwnerID, ids)
}

func (u eraseUnit) exec(ctx context.Context, ids []string, statements ...string) error {
	for _, sql := range statements {
		if _, err := u.tx.Exec(ctx, sql, u.owner(), ids); err != nil {
			return err
		}
	}
	return nil
}

func (u eraseUnit) DropAnswers(ctx context.Context, ids []string, receipt string) error {
	if _, err := u.tx.Exec(ctx,
		// Retain only the opaque owner/agent-bound ID as a deletion tombstone.
		// Open cards can skip this turn without accepting forged history IDs.
		// The original desk/capture/incomplete source is not a recalled dependency. Its
		// ExternalID links it to the request even when the model was unavailable.
		`UPDATE desk_turns SET question='',answer='',dependencies='[]'::jsonb,
         response=CASE WHEN response IS NULL THEN NULL ELSE jsonb_set(response,'{turn}',
          (response->'turn') || jsonb_build_object('text','','reply','','cards','[]'::jsonb,'ask',NULL,
           'receipts',coalesce((SELECT jsonb_agg((r - 'reason') || jsonb_build_object('text',$3::text) ORDER BY ord)
             FROM jsonb_array_elements(coalesce(response->'turn'->'receipts','[]'::jsonb)) WITH ORDINALITY AS receipt(r,ord)), '[]'::jsonb))) END
         WHERE owner_id=$1 AND (
          EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(nullif(dependencies,'null'::jsonb),'[]'::jsonb)) d WHERE d->>'id'=ANY($2::text[]))
          OR EXISTS(SELECT 1 FROM sources s WHERE s.owner_id=$1 AND s.id=ANY($2::uuid[])
            AND s.connector IN ('desk','capture','desk-incomplete') AND lower(s.external_id)=desk_turns.request_id::text))`, u.owner(), ids, receipt); err != nil {
		return err
	}
	return u.exec(ctx, ids,
		`DELETE FROM work_documents WHERE owner_id=$1 AND document->>'runId' IN (SELECT run_id::text FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`,
		`DELETE FROM training_samples WHERE owner_id=$1 AND (memory_id=ANY($2::uuid[]) OR run_id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[])))`,
		`INSERT INTO background_usage(owner_id,id,job_id,reserved_cost,created_at) SELECT owner_id,md5(owner_id::text || ':' || id::text || ':' || coalesce(document->>'createdAt',''))::uuid,NULL,CASE WHEN status IN ('queued','waiting') THEN 0 ELSE reserved_cost END,created_at FROM agent_runs WHERE owner_id=$1 AND id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`,
		`DELETE FROM agent_runs WHERE owner_id=$1 AND id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`,
	)
}

func (u eraseUnit) Detach(ctx context.Context, ids []string) error {
	return u.exec(ctx, ids,
		`INSERT INTO blob_cleanup_jobs(owner_id,blob_key) SELECT owner_id,blob_key FROM source_versions WHERE owner_id=$1 AND source_id=ANY($2::uuid[]) AND blob_key IS NOT NULL ON CONFLICT DO NOTHING`,
		`DELETE FROM capture_candidates WHERE owner_id=$1 AND document->>'resolvedInto'=ANY($2::text[])`,
		`DELETE FROM evidence WHERE owner_id=$1 AND (source_id=ANY($2::uuid[]) OR target_id=ANY($2::uuid[]))`,
		`DELETE FROM episode_members WHERE owner_id=$1 AND member_id=ANY($2::uuid[])`,
		`DELETE FROM derived_dependencies WHERE owner_id=$1 AND dependency_id=ANY($2::uuid[])`,
		`DELETE FROM relations WHERE owner_id=$1 AND (from_id=ANY($2::uuid[]) OR to_id=ANY($2::uuid[]))`,
		`DELETE FROM claim_revisions WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`,
		`UPDATE desk_turns SET response=response - 'attachmentContext' WHERE owner_id=$1 AND request_id IN (SELECT request_id FROM desk_attachments WHERE owner_id=$1 AND source_id=ANY($2::uuid[]))`,
		`DELETE FROM source_versions WHERE owner_id=$1 AND source_id=ANY($2::uuid[])`,
	)
}

func (u eraseUnit) Items(ctx context.Context) ([]workspace.Item, error) {
	return kernel.QueryDocuments[workspace.Item](ctx, u.tx, "SELECT document FROM work_items WHERE owner_id=$1", u.owner())
}

func (u eraseUnit) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, u.tx, u.scope, item)
}

func (u eraseUnit) Remove(ctx context.Context, ids []string) error {
	_, err := u.tx.Exec(ctx, "DELETE FROM memory_records WHERE owner_id=$1 AND id=ANY($2::uuid[])", u.owner(), ids)
	return err
}
