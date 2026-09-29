package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Keep old vectors for rollback. Recall compares only the selected model.
func (s *Store) QueueEmbeddingBackfill(ctx context.Context, scope memory.Scope) (int64, error) {
	if err := requireOwner(scope); err != nil {
		return 0, err
	}
	if s.models == nil || !s.models.Available(s.models.EmbeddingID()) {
		return 0, memory.ErrUnavailable
	}
	p, ok := s.models.Get(s.models.EmbeddingID())
	if !ok {
		return 0, memory.ErrUnavailable
	}
	model := p.ID + ":" + p.Model
	stage := fmt.Sprintf("memory.embed:%x", sha256.Sum256([]byte(model)))
	tag, err := s.pool.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage)
		SELECT gen_random_uuid(),r.owner_id,r.id,r.version,$2
		FROM memory_records r JOIN memory_text t ON (t.owner_id,t.id,t.version)=(r.owner_id,r.id,r.version)
		WHERE r.owner_id=$1 AND r.state='active' AND (
		 (r.kind='claim' AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE (e.owner_id,e.record_id,e.record_version)=(r.owner_id,r.id,r.version) AND e.model=$3))
		 OR (r.kind='source' AND EXISTS(SELECT 1 FROM chunks c WHERE (c.owner_id,c.source_id,c.source_version)=(r.owner_id,r.id,r.version)
		  AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE (e.owner_id,e.record_id,e.record_version)=(c.owner_id,c.id,c.version) AND e.model=$3))))
		ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET state='queued',attempts=0,error_code='',available_at=now(),lease_token=NULL,lease_until=NULL,updated_at=now()
		WHERE memory_jobs.state IN ('done','blocked','failed')`, string(scope.OwnerID), stage, model)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
