package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type secretaryArtifactKey struct{}
type secretaryArtifactContext struct {
	Task         memory.TrustedTaskContext
	Dependencies []memory.TypedDependency
}

func loadItemBlocksTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing string) (map[string][]artifactBlock, error) {
	type fieldBlocks struct {
		Field  string          `json:"field"`
		Blocks []artifactBlock `json:"blocks"`
	}
	fields, err := queryDocuments[fieldBlocks](ctx, tx, "SELECT jsonb_build_object('field',field,'blocks',blocks) FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 ORDER BY field", string(scope.OwnerID), thing)
	out := map[string][]artifactBlock{}
	for _, f := range fields {
		out[f.Field] = f.Blocks
	}
	return out, err
}
func itemBlocksHashTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing string) (string, error) {
	fields, err := loadItemBlocksTx(ctx, tx, scope, thing)
	if err != nil {
		return "", err
	}
	normalized := map[string][]artifactBlock{}
	for field, blocks := range fields {
		// Audit revisions are intentionally excluded by action_document_hash too:
		// undo appends a receipt without changing the restored authored content.
		if strings.HasPrefix(field, "history:") || strings.HasPrefix(field, "evolution:") || strings.HasPrefix(field, "source:") {
			continue
		}
		next := []artifactBlock{}
		for _, block := range blocks {
			if block.Text == "" {
				continue
			}
			block.Runs = append([]string{}, block.Runs...)
			block.DeskActions = append([]string{}, block.DeskActions...)
			sort.Strings(block.Runs)
			sort.Strings(block.DeskActions)
			if len(next) > 0 && strings.Join(next[len(next)-1].Runs, ",") == strings.Join(block.Runs, ",") && strings.Join(next[len(next)-1].DeskActions, ",") == strings.Join(block.DeskActions, ",") {
				next[len(next)-1].Text += block.Text
			} else {
				next = append(next, block)
			}
		}
		if len(next) > 0 {
			normalized[field] = next
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256(asJSON(normalized))), nil
}

// Capture before either blocks or the canonical item change. The normal item
// trigger retains this snapshot and supplies the final document fingerprint.
func recordActionBlocksBeforeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing string) error {
	if _, ok := ctx.Value(actionLogKey{}).(actionLog); !ok {
		return nil
	}
	var raw string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.action_changes',true),'')").Scan(&raw); err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	var changes []actionChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		return err
	}
	index := -1
	for i, c := range changes {
		if c.Table == "work_items" && c.ID == thing {
			if c.BeforeBlocks != nil {
				return nil
			}
			index = i
			break
		}
	}
	blocks, err := loadItemBlocksTx(ctx, tx, scope, thing)
	if err != nil {
		return err
	}
	if index < 0 {
		before := json.RawMessage("null")
		err := tx.QueryRow(ctx, "SELECT document FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), thing).Scan(&before)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		changes = append(changes, actionChange{Table: "work_items", ID: thing, Before: before, BeforeBlocks: blocks})
	} else {
		changes[index].BeforeBlocks = blocks
	}
	_, err = tx.Exec(ctx, "SELECT set_config('pcas.action_changes',$1,true)", string(asJSON(changes)))
	return err
}

// Called only with a server-owned secretary context. Unchanged owner fields
// remain independent; append operations mark just the appended range.
func syncSecretaryArtifactEditsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item, previous workspace.Item) error {
	provenance, ok := ctx.Value(secretaryArtifactKey{}).(secretaryArtifactContext)
	log, logged := ctx.Value(actionLogKey{}).(actionLog)
	if !ok || !logged || !memory.ID(log.id).Valid() {
		return nil
	}
	if err := verifyTypedContextTx(ctx, tx, scope, provenance.Task, provenance.Dependencies); err != nil {
		return err
	}
	oldFields := itemArtifactText(previous)
	blocks, err := loadItemBlocksTx(ctx, tx, scope, item.ID)
	if err != nil {
		return err
	}
	for field, text := range itemArtifactText(item) {
		old := oldFields[field]
		if text == old {
			continue
		}
		next := []artifactBlock{{Text: text, Runs: []string{}, DeskActions: []string{log.id}}}
		if oneOf(field, "notes", "body", "goal") && old != "" && strings.HasPrefix(text, old) {
			before, exists := blocks[field]
			if !exists {
				before = []artifactBlock{{Text: old, Runs: []string{}}}
			}
			// syncArtifactEdits may already have appended owner-labelled text. Use the
			// old canonical range, retaining all original labels, before adding ours.
			before = editBlocks(before, old)
			next = append(before, artifactBlock{Text: strings.TrimPrefix(text, old), Runs: []string{}, DeskActions: []string{log.id}})
		}
		if err := saveBlocksTx(ctx, tx, scope, item.ID, field, next); err != nil {
			return err
		}
	}
	return nil
}

func appendTaskDeskActions(task *memory.TrustedTaskContext, ids ...memory.ID) error {
	seen := map[memory.ID]bool{}
	for _, id := range append(append([]memory.ID{}, task.DeskActions...), ids...) {
		if !id.Valid() {
			return memory.ErrConflict
		}
		seen[id] = true
	}
	if len(seen) > 256 {
		return memory.ErrRecordCapacity
	}
	next := make([]memory.ID, 0, len(seen))
	for id := range seen {
		next = append(next, id)
	}
	sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
	task.DeskActions = next
	return nil
}
func sameDeskActions(a, b []memory.ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i, id := range a {
		if id != b[i] {
			return false
		}
	}
	return true
}

type secretaryOriginGraph struct {
	visiting map[string]bool
	seen     map[string]bool
	done     map[string][]memory.TypedDependency
}

func newSecretaryOriginGraph() *secretaryOriginGraph {
	return &secretaryOriginGraph{visiting: map[string]bool{}, seen: map[string]bool{}, done: map[string][]memory.TypedDependency{}}
}
func (s *Store) secretaryArtifactOriginTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, actionID string, target *memory.TrustedTaskContext) ([]memory.TypedDependency, error) {
	return s.secretaryArtifactOriginGraphTx(ctx, tx, scope, actionID, target, newSecretaryOriginGraph(), 0)
}
func (s *Store) secretaryArtifactOriginGraphTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, actionID string, target *memory.TrustedTaskContext, graph *secretaryOriginGraph, depth int) ([]memory.TypedDependency, error) {
	if !memory.ID(actionID).Valid() {
		return nil, memory.ErrConflict
	}
	if graph.visiting[actionID] {
		return nil, memory.ErrRecordCapacity
	}
	if depth >= 16 {
		return nil, memory.ErrRecordCapacity
	}
	if deps, ok := graph.done[actionID]; ok {
		return deps, nil
	}
	graph.seen[actionID] = true
	if len(graph.seen) > 256 {
		return nil, memory.ErrRecordCapacity
	}
	graph.visiting[actionID] = true
	defer delete(graph.visiting, actionID)
	var stored memory.TrustedTaskContext
	var deps []memory.TypedDependency
	log, logged := ctx.Value(actionLogKey{}).(actionLog)
	current, present := ctx.Value(secretaryArtifactKey{}).(secretaryArtifactContext)
	if logged && present && log.id == actionID {
		stored, deps = current.Task, current.Dependencies
	} else {
		var raw []byte
		var stale bool
		err := tx.QueryRow(ctx, "SELECT context_task,context_stale OR undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), actionID).Scan(&raw, &stale)
		if err != nil {
			return nil, typedReadError(err)
		}
		if stale || len(raw) == 0 || json.Unmarshal(raw, &stored) != nil {
			return nil, memory.ErrConflict
		}
		deps, err = loadContextArtifactDependenciesTx(ctx, tx, scope, "artifact", actionID, 1)
		if err != nil {
			return nil, err
		}
	}
	if stored.OwnerID != scope.OwnerID || stored.Recipient.Role != "secretary" {
		return nil, memory.ErrConflict
	}
	actual, err := s.contextRecipientModelTx(ctx, tx, scope, stored.Recipient.PrincipalID, stored.Recipient.Role, nil, stored.Recipient.Model)
	if err != nil || actual != stored.Recipient {
		return nil, memory.ErrConflict
	}
	if err := verifyTypedContextTx(ctx, tx, scope, stored, deps); err != nil {
		return nil, err
	}
	all := []memory.TypedDependency{}
	for _, parent := range stored.DeskActions {
		inherited, err := s.secretaryArtifactOriginGraphTx(ctx, tx, scope, string(parent), target, graph, depth+1)
		if err != nil {
			return nil, err
		}
		all = mergeRunDependencies(all, inherited)
	}
	if target == nil {
		if !scope.IsOwner {
			return nil, memory.ErrForbidden
		}
		all = mergeRunDependencies(all, deps)
	} else {
		entries, cov, err := hydrateTypedContextTx(ctx, tx, scope, *target, refsForDependencies(deps))
		if err != nil {
			return nil, err
		}
		if !cov.Complete || len(entries) != len(deps) {
			return nil, memory.ErrForbidden
		}
		all = mergeRunDependencies(all, dependenciesForEntries(entries))
	}
	graph.done[actionID] = all
	return all, nil
}
func (s *Store) verifyTaskDeskActionsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, task memory.TrustedTaskContext) error {
	if len(task.DeskActions) > 256 {
		return memory.ErrRecordCapacity
	}
	graph := newSecretaryOriginGraph()
	for _, id := range task.DeskActions {
		if _, err := s.secretaryArtifactOriginGraphTx(ctx, tx, scope, string(id), &task, graph, 0); err != nil {
			return err
		}
	}
	return nil
}

type verifiedDeskOriginsKey struct{}

func (s *Store) verifyContextAttemptTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id memory.ID, task memory.TrustedTaskContext, deps []memory.TypedDependency) error {
	if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
		return err
	}
	if err := s.verifyTaskDeskActionsTx(ctx, tx, scope, task); err != nil {
		return err
	}
	return verifyContextAttemptTx(context.WithValue(ctx, verifiedDeskOriginsKey{}, true), tx, scope, id, task, deps)
}

func (s *Store) filterSecretaryBlocksTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, blocks []artifactBlock) ([]artifactBlock, []memory.Ref, error) {
	kept := []artifactBlock{}
	refs := []memory.Ref{}
	for _, block := range blocks {
		allowed := true
		blockRefs := []memory.Ref{}
		for _, id := range block.DeskActions {
			deps, err := s.secretaryArtifactOriginTx(ctx, tx, scope, id, scope.Task)
			if err != nil {
				if isSecretaryOriginGap(err) {
					allowed = false
					break
				}
				return nil, nil, err
			}
			blockRefs = append(blockRefs, refsForDependencies(deps)...)
		}
		if allowed && scope.Task != nil {
			ids := []memory.ID{}
			for _, id := range block.DeskActions {
				ids = append(ids, memory.ID(id))
			}
			if err := appendTaskDeskActions(scope.Task, ids...); err != nil {
				return nil, nil, err
			}
		}
		if allowed {
			refs = append(refs, blockRefs...)
			kept = append(kept, block)
		}
	}
	return kept, refs, nil
}
func isSecretaryOriginGap(err error) bool {
	return errors.Is(err, memory.ErrForbidden) || errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrNotFound) || errors.Is(err, memory.ErrUnavailable) || errors.Is(err, memory.ErrInvalid)
}

// Restore only still-live generated ranges. Both Run and Desk origins remain
// server facts; the caller has already fenced the undo's final document/blocks.
func (s *Store) restoreActionBlocksTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item *workspace.Item, before map[string][]artifactBlock) (bool, error) {
	scrubbed := false
	if _, err := tx.Exec(ctx, "DELETE FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID); err != nil {
		return false, err
	}
	review := scope
	review.Task = nil
	for field, blocks := range before {
		kept, _, err := s.filterSecretaryBlocksTx(ctx, tx, review, blocks)
		if err != nil {
			return false, err
		}
		live := []artifactBlock{}
		for _, block := range kept {
			allowed := true
			for _, runID := range block.Runs {
				run, err := queryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), runID)
				if err != nil || s.verifyRunForItemTx(ctx, tx, review, run, item) != nil {
					allowed = false
					break
				}
			}
			if allowed {
				live = append(live, block)
			}
		}
		if blockText(live) != blockText(blocks) {
			scrubbed = true
		}
		text := blockText(live)
		if field == "title" && text == "" {
			text = "内容已失效"
			live = []artifactBlock{{Text: text, Runs: []string{}}}
		}
		setField(item, field, text)
		if err := saveBlocksTx(ctx, tx, scope, item.ID, field, live); err != nil {
			return false, err
		}
	}
	return scrubbed, nil
}

// Clear all live copies of an invalidated action, including promoted blocks.
// The action's task and dependency skeleton survive; only prose/undo bodies go.
func purgeSecretaryArtifactsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ids []string, recipient any) ([]string, error) {
	actionIDs, err := queryDocuments[string](ctx, tx, `WITH RECURSIVE affected(id) AS (
 SELECT l.id FROM action_log l WHERE l.owner_id=$1 AND l.context_task IS NOT NULL AND EXISTS(SELECT 1 FROM context_artifact_dependencies d WHERE d.owner_id=l.owner_id AND d.parent_kind='artifact' AND d.parent_id=l.id::text AND d.parent_version=1 AND d.dependency_id=ANY($2::uuid[]) AND ($3::jsonb IS NULL OR d.recipient=$3))
 UNION SELECT child.id FROM affected a JOIN action_log child ON child.owner_id=$1 AND (child.context_task->'desk_actions') ? a.id::text
 ) SELECT to_jsonb(id::text) FROM affected`, string(scope.OwnerID), ids, recipient)
	if err != nil || len(actionIDs) == 0 {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE action_log SET context_stale=true,summary='动作依据已变更',changes='[]'::jsonb,expired_at=coalesce(expired_at,now()) WHERE owner_id=$1 AND id=ANY($2::uuid[])`, string(scope.OwnerID), actionIDs); err != nil {
		return nil, err
	}
	things, err := queryDocuments[string](ctx, tx, `SELECT DISTINCT to_jsonb(thing_id::text) FROM artifact_fields f WHERE owner_id=$1 AND EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(blocks)='array' THEN blocks ELSE '[]'::jsonb END) b WHERE (b->'deskActions') ?| $2::text[])`, string(scope.OwnerID), actionIDs)
	if err != nil {
		return nil, err
	}
	for _, thing := range things {
		item, err := getItem(ctx, tx, scope, thing)
		if err != nil {
			return nil, err
		}
		oldBlocksHash, err := itemBlocksHashTx(ctx, tx, scope, thing)
		if err != nil {
			return nil, err
		}
		var oldHash string
		if err := tx.QueryRow(ctx, "SELECT action_document_hash(document) FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), thing).Scan(&oldHash); err != nil {
			return nil, err
		}
		fields, err := loadItemBlocksTx(ctx, tx, scope, thing)
		if err != nil {
			return nil, err
		}
		for field, blocks := range fields {
			kept := []artifactBlock{}
			changed := false
			for _, block := range blocks {
				remove := false
				for _, id := range block.DeskActions {
					if oneOf(id, actionIDs...) {
						remove = true
						break
					}
				}
				if remove {
					changed = true
				} else {
					kept = append(kept, block)
				}
			}
			if !changed {
				continue
			}
			text := blockText(kept)
			if field == "title" && text == "" {
				text = "内容已失效"
				kept = []artifactBlock{{Text: text, Runs: []string{}}}
			}
			setField(&item, field, text)
			if err := saveBlocksTx(ctx, tx, scope, thing, field, kept); err != nil {
				return nil, err
			}
		}
		item.Version++
		item.UpdatedAt = stamp()
		if err := saveItem(ctx, tx, scope, item); err != nil {
			return nil, err
		}
		if err := sanitizeActionSnapshotsTx(ctx, tx, scope, thing, actionIDs, oldHash, oldBlocksHash); err != nil {
			return nil, err
		}
	}
	sourceIDs, err := queryDocuments[string](ctx, tx, `SELECT to_jsonb(id::text) FROM sources WHERE owner_id=$1 AND connector='actions' AND external_id=ANY($2::text[])`, string(scope.OwnerID), things)
	if err != nil {
		return nil, err
	}
	if len(sourceIDs) > 0 {
		if _, err = tx.Exec(ctx, `UPDATE source_versions SET body='',title='动作依据已变更' WHERE owner_id=$1 AND source_id=ANY($2::uuid[])`, string(scope.OwnerID), sourceIDs); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE chunks SET body='',search_text='' WHERE owner_id=$1 AND source_id=ANY($2::uuid[])`, string(scope.OwnerID), sourceIDs); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM embeddings WHERE owner_id=$1 AND record_id IN (SELECT id FROM chunks WHERE owner_id=$1 AND source_id=ANY($2::uuid[]))`, string(scope.OwnerID), sourceIDs); err != nil {
			return nil, err
		}
	}
	return sourceIDs, nil
}

func sanitizeActionSnapshotsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing string, invalid []string, oldHash, oldBlocksHash string) error {
	type loggedChanges struct {
		ID      string         `json:"id"`
		Changes []actionChange `json:"changes"`
	}
	logs, err := queryDocuments[loggedChanges](ctx, tx, `SELECT jsonb_build_object('id',id,'changes',changes) FROM action_log WHERE owner_id=$1 AND expired_at IS NULL AND EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(changes)='array' THEN changes ELSE '[]'::jsonb END) c WHERE c->>'table'='work_items' AND c->>'id'=$2)`, string(scope.OwnerID), thing)
	if err != nil {
		return err
	}
	var newHash string
	if err := tx.QueryRow(ctx, "SELECT action_document_hash(document) FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), thing).Scan(&newHash); err != nil {
		return err
	}
	newBlocksHash, err := itemBlocksHashTx(ctx, tx, scope, thing)
	if err != nil {
		return err
	}
	for _, log := range logs {
		ambiguous := false
		for i := range log.Changes {
			change := &log.Changes[i]
			if change.Table != "work_items" || change.ID != thing {
				continue
			}
			if change.BeforeBlocks == nil {
				ambiguous = true
				break
			}
			var before workspace.Item
			if string(change.Before) != "null" && json.Unmarshal(change.Before, &before) != nil {
				return memory.ErrInvalid
			}
			for field, blocks := range change.BeforeBlocks {
				kept := []artifactBlock{}
				removed := false
				for _, block := range blocks {
					affected := false
					for _, id := range block.DeskActions {
						if oneOf(id, invalid...) {
							affected = true
							break
						}
					}
					if affected {
						removed = true
					} else {
						kept = append(kept, block)
					}
				}
				if !removed {
					continue
				}
				text := blockText(kept)
				if field == "title" && text == "" {
					text = "内容已失效"
					kept = []artifactBlock{{Text: text, Runs: []string{}}}
				}
				change.BeforeBlocks[field] = kept
				if string(change.Before) != "null" {
					setField(&before, field, text)
				}
			}
			if string(change.Before) != "null" {
				change.Before = asJSON(before)
			}
			// Rebase only the precise, unsuperseded server state; ordinary document
			// edits or provenance edits independently defeat this dual comparison.
			if change.AfterHash != nil && *change.AfterHash == oldHash && change.AfterBlocksHash != nil && *change.AfterBlocksHash == oldBlocksHash {
				documentFence, blockFence := newHash, newBlocksHash
				change.AfterHash = &documentFence
				change.AfterBlocksHash = &blockFence
			}
		}
		if ambiguous {
			if _, err := tx.Exec(ctx, "UPDATE action_log SET changes='[]'::jsonb,expired_at=coalesce(expired_at,now()),summary='动作依据已变更' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), log.ID); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, "UPDATE action_log SET changes=$3,summary='事项修改' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), log.ID, asJSON(log.Changes)); err != nil {
				return err
			}
		}
	}
	return nil
}
