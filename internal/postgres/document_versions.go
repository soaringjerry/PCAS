package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"strings"
	"time"
)

func documentVersionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string, version int) (workspace.DocumentVersion, error) {
	out := workspace.DocumentVersion{DocumentID: id, Version: version}
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT v.document->>'title',v.document->>'body',v.written_at,v.author,v.based_on,v.run_id::text FROM document_versions v JOIN work_documents d ON(d.owner_id,d.id)=(v.owner_id,v.document_id) WHERE v.owner_id=$1 AND v.document_id=$2 AND v.version=$3`, string(scope.OwnerID), id, version).Scan(&out.Title, &out.Body, &at, &out.Author, &out.BasedOn, &out.RunID)
	if err == pgx.ErrNoRows {
		return out, memory.ErrNotFound
	}
	out.WrittenAt = at.UTC().Format(time.RFC3339Nano)
	return out, err
}
func (s *Store) ReadDocumentVersion(ctx context.Context, scope memory.Scope, id string, version int) (workspace.DocumentVersion, error) {
	var out workspace.DocumentVersion
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(id).Valid() || version < 1 {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = documentVersionTx(ctx, tx, scope, id, version)
		return err
	})
	return out, err
}
func (s *Store) ListDocumentVersions(ctx context.Context, scope memory.Scope, id string) (workspace.DocumentVersionList, error) {
	out := workspace.DocumentVersionList{Items: []workspace.DocumentVersionSummary{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(id).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT (document->>'version')::integer FROM work_documents WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id).Scan(&out.CurrentVersion); err == pgx.ErrNoRows {
			return memory.ErrNotFound
		} else if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT version,written_at,author,based_on,run_id::text FROM document_versions WHERE owner_id=$1 AND document_id=$2 ORDER BY written_at DESC,version DESC`, string(scope.OwnerID), id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v := workspace.DocumentVersionSummary{DocumentID: id}
			var at time.Time
			if err := rows.Scan(&v.Version, &at, &v.Author, &v.BasedOn, &v.RunID); err != nil {
				return err
			}
			v.WrittenAt = at.UTC().Format(time.RFC3339Nano)
			out.Items = append(out.Items, v)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) ReadDocumentDiff(ctx context.Context, scope memory.Scope, id string, from, to int) (workspace.DocumentDiff, error) {
	out := workspace.DocumentDiff{DocumentID: id, FromVersion: from, ToVersion: to, Changes: []workspace.ParagraphChange{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(id).Valid() || from < 0 || to < 0 || (from == 0) != (to == 0) {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		if to == 0 {
			var current int
			if err := tx.QueryRow(ctx, `SELECT (document->>'version')::integer FROM work_documents WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id).Scan(&current); err == pgx.ErrNoRows {
				return memory.ErrNotFound
			} else if err != nil {
				return err
			}
			to = current
			from = max(1, to-1)
		}
		a, err := documentVersionTx(ctx, tx, scope, id, from)
		if err != nil {
			return err
		}
		b, err := documentVersionTx(ctx, tx, scope, id, to)
		if err != nil {
			return err
		}
		out.FromVersion, out.ToVersion = from, to
		out.Changes = paragraphDiff(a.Body, b.Body)
		return nil
	})
	return out, err
}
func paragraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := []string{}
	var current []string
	flush := func() {
		if len(current) > 0 {
			parts = append(parts, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
		} else {
			current = append(current, line)
		}
	}
	flush()
	return parts
}

// Linear-space LCS (Hirschberg) avoids a quadratic allocation for long writing.
// It compares literal paragraphs; no semantic judgment is made here.
type paragraphPair struct{ a, b int }

func lcsRow(a, b []string) []int {
	row := make([]int, len(b)+1)
	for _, x := range a {
		prev := 0
		for j, y := range b {
			old := row[j+1]
			if x == y {
				row[j+1] = prev + 1
			} else {
				row[j+1] = max(row[j], row[j+1])
			}
			prev = old
		}
	}
	return row
}
func reverseStrings(a []string) []string {
	b := make([]string, len(a))
	for i := range a {
		b[len(a)-1-i] = a[i]
	}
	return b
}
func paragraphPairs(a, b []string, ai, bi int) []paragraphPair {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	if len(a) == 1 {
		for j := range b {
			if a[0] == b[j] {
				return []paragraphPair{{ai, bi + j}}
			}
		}
		return nil
	}
	half := len(a) / 2
	left := lcsRow(a[:half], b)
	right := lcsRow(reverseStrings(a[half:]), reverseStrings(b))
	split, best := 0, -1
	for j := 0; j <= len(b); j++ {
		n := left[j] + right[len(b)-j]
		if n > best {
			split, best = j, n
		}
	}
	return append(paragraphPairs(a[:half], b[:split], ai, bi), paragraphPairs(a[half:], b[split:], ai+half, bi+split)...)
}
func paragraphDiff(before, after string) []workspace.ParagraphChange {
	a, b := paragraphs(before), paragraphs(after)
	out := []workspace.ParagraphChange{}
	pairs := append(paragraphPairs(a, b, 0, 0), paragraphPair{len(a), len(b)})
	i, j := 0, 0
	for _, p := range pairs {
		for i < p.a || j < p.b {
			c := workspace.ParagraphChange{}
			if i < p.a && j < p.b {
				x, y := i, j
				c.Kind = "change"
				c.BeforeIndex, c.AfterIndex = &x, &y
				c.Before, c.After = a[i], b[j]
				i++
				j++
			} else if i < p.a {
				x := i
				c.Kind = "delete"
				c.BeforeIndex = &x
				c.Before = a[i]
				i++
			} else {
				y := j
				c.Kind = "add"
				c.AfterIndex = &y
				c.After = b[j]
				j++
			}
			out = append(out, c)
		}
		i, j = p.a+1, p.b+1
	}
	return out
}
func restoreDocumentTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, doc workspace.Doc) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('pcas.restore_document',$1,true)`, doc.ID); err != nil {
		return err
	}
	err := saveDoc(ctx, tx, scope, doc)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT set_config('pcas.restore_document','',true)`)
	return err
}
