package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Resolve old facet IDs as well as alias names: bookmarks using the original
// topic ID keep working after that topic is merged into a place/organization.
// Called before both the count and page query, in the same read transaction.
func resolveMemoryEntityFiltersTx(ctx context.Context, tx pgx.Tx, owner memory.ID, opts *memoryReadOptions) error {
	for _, field := range []*string{&opts.query.Entity, &opts.query.Group} {
		if *field == "" {
			continue
		}
		id, err := resolveMergedEntityTx(ctx, tx, owner, memory.ID(*field))
		if err != nil {
			return err
		}
		*field = string(id)
	}
	if opts.query.Group == "" {
		return nil
	}
	var kind string
	err := tx.QueryRow(ctx, `SELECT ev.entity_type FROM entity_versions ev JOIN memory_records r ON(r.owner_id,r.id,r.version)=(ev.owner_id,ev.entity_id,ev.version) WHERE r.owner_id=$1 AND r.id=$2 AND r.state='active'`, string(owner), opts.query.Group).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	opts.groupAsEntity = oneOf(kind, "place", "organization")
	return err
}
