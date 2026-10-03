package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The library lists where material came from, not every record: everything
// said to the secretary is one entry, each import is one entry, and a document
// stands on its own. Records the system writes about its own actions are not
// material and stay out.
func sourceGroupsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) ([]workspace.Source, error) {
	// An import is counted from its archive's listing, in one pass; only the
	// material outside archives, which stays small, is read row by row.
	rows, err := tx.Query(ctx, `WITH bad AS (SELECT DISTINCT record_id FROM memory_jobs WHERE owner_id=$1 AND state IN ('failed','blocked')),
 member AS (SELECT DISTINCT source_id FROM archive_entries WHERE owner_id=$1),
 root AS (SELECT DISTINCT archive_id FROM archive_entries WHERE owner_id=$1),
 imports AS (
  SELECT 'import:'||ae.archive_id::text AS key,'import' AS connector,count(*) AS items,max(r.updated_at) AS at,'' AS title,
   bool_or(ae.source_id IN (SELECT record_id FROM bad)) AS blocked,false AS pending,ae.archive_id
  FROM archive_entries ae JOIN memory_records r ON (r.owner_id,r.id)=(ae.owner_id,ae.source_id)
  WHERE ae.owner_id=$1 AND r.state='active' AND r.version=ae.source_version GROUP BY ae.archive_id),
 rest AS (
  SELECT CASE WHEN s.connector IN ('desk','desk-incomplete') THEN 'said' WHEN s.connector IN ('memory-input','telegram','capture') THEN s.connector ELSE s.id::text END AS key,
   s.connector,s.id,r.updated_at,v.title
  FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
  JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version)
  WHERE s.owner_id=$1 AND r.state='active' AND s.connector NOT IN ('actions','corrections')
   AND s.id NOT IN (SELECT source_id FROM member) AND s.id NOT IN (SELECT archive_id FROM root)),
 others AS (
  SELECT k.key,min(k.connector) AS connector,count(*) AS items,max(k.updated_at) AS at,min(k.title) AS title,
   bool_or(k.id IN (SELECT record_id FROM bad)) AS blocked,
   bool_or(EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=$1 AND j.record_id=k.id AND j.state IN ('queued','leased'))) AS pending,NULL::uuid AS archive_id
  FROM rest k GROUP BY k.key)
SELECT g.key,g.connector,g.items,g.at,g.title,g.blocked,g.pending,
 coalesce((SELECT v.title FROM memory_records r JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version)
   WHERE r.owner_id=$1 AND r.id=g.archive_id),'')
FROM (SELECT * FROM imports UNION ALL SELECT * FROM others) g ORDER BY g.at DESC,g.key`, string(scope.OwnerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []workspace.Source{}
	for rows.Next() {
		var source workspace.Source
		var connector, title, archive string
		var at time.Time
		var blocked, pending bool
		if err := rows.Scan(&source.ID, &connector, &source.ItemCount, &at, &title, &blocked, &pending, &archive); err != nil {
			return nil, err
		}
		switch {
		case strings.HasPrefix(source.ID, "import:"):
			source.Kind, source.Name = "import", archive
			if source.Name == "" {
				source.Name = "导入的聊天记录"
			}
		case source.ID == "said":
			source.Kind, source.Name = "said", "跟秘书说的话"
		case source.ID == "memory-input":
			source.Kind, source.Name = "note", "添加记忆时写的话"
		case source.ID == "capture":
			source.Kind, source.Name = "capture", "快速记录"
		case source.ID == "telegram":
			source.Kind, source.Name = "telegram", "从 Telegram 发来的"
		default:
			source.Kind, source.Name, source.Single = "file", title, true
			if strings.TrimSpace(source.Name) == "" {
				source.Name = "没有标题的资料"
			}
		}
		source.Status = "connected"
		// Background work on some of many originals is ordinary and says
		// nothing about the entry; only a single document reports it.
		if pending && source.Single {
			source.Status, source.Note = "syncing", "原文可读，还在处理"
		}
		if blocked {
			source.Status, source.Note = "failed", "原文可读，部分处理未完成；展开后台任务查看缺口"
		}
		source.LastSyncAt = at.UTC().Format(time.RFC3339Nano)
		out = append(out, source)
	}
	return out, rows.Err()
}

// SourceGroupItems pages through one library entry, newest first. Each item
// is one stored original; the page opens it by ID like any other original.
// find keeps only the originals whose words or title contain it.
func (s *Store) SourceGroupItems(ctx context.Context, scope memory.Scope, key, find, cursor string, limit int) (workspace.SourceItems, error) {
	out := workspace.SourceItems{Items: []workspace.SourceItem{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	archive, grouped := strings.CutPrefix(key, "import:")
	if grouped && !memory.ID(archive).Valid() || !grouped && !oneOf(key, "said", "memory-input", "telegram", "capture") {
		return out, memory.ErrInvalid
	}
	find = strings.TrimSpace(find)
	if len(find) > 200 {
		return out, memory.ErrInvalid
	}
	pattern := ""
	if find != "" {
		pattern = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(find) + "%"
	}
	before, beforeID := time.Now().UTC().Add(24*time.Hour), "ffffffff-ffff-ffff-ffff-ffffffffffff"
	if cursor != "" {
		at, id, ok := strings.Cut(cursor, "|")
		parsed, err := time.Parse(time.RFC3339Nano, at)
		if !ok || err != nil || !memory.ID(id).Valid() {
			return out, memory.ErrInvalid
		}
		before, beforeID = parsed, id
	}
	// The originals of one entry are found from where that entry lives: an
	// archive's listing, or the handful of connectors that are grouped.
	from := `sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
 WHERE s.owner_id=$1 AND r.state='active' AND s.connector=ANY($2::text[])
  AND NOT EXISTS(SELECT 1 FROM archive_entries ae WHERE ae.owner_id=s.owner_id AND ae.source_id=s.id)`
	var member any = []string{key}
	if key == "said" {
		member = []string{"desk", "desk-incomplete"}
	}
	if grouped {
		from = `archive_entries ae JOIN memory_records r ON (r.owner_id,r.id)=(ae.owner_id,ae.source_id)
 WHERE ae.owner_id=$1 AND ae.archive_id=$2::uuid AND r.state='active' AND r.version=ae.source_version`
		member = archive
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id::text,v.title,left(v.body,160),coalesce(c.role,''),coalesce(rv.expressed_at,r.created_at) AS at
FROM (SELECT r.id,r.version,r.created_at FROM `+from+`) r
JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=($1,r.id,r.version)
JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=($1,r.id,r.version)
LEFT JOIN source_contexts c ON (c.owner_id,c.source_id,c.source_version)=($1,r.id,r.version)
WHERE (coalesce(rv.expressed_at,r.created_at),r.id)<($3,$4::uuid)
 AND ($6='' OR v.body ILIKE $6 ESCAPE '\' OR v.title ILIKE $6 ESCAPE '\')
ORDER BY at DESC,r.id DESC LIMIT $5`, string(scope.OwnerID), member, before, beforeID, limit+1, pattern)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item workspace.SourceItem
		var at time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.Excerpt, &item.Role, &at); err != nil {
			return out, err
		}
		item.At = at.UTC().Format(time.RFC3339Nano)
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		out.Next = last.At + "|" + last.ID
	}
	return out, nil
}
