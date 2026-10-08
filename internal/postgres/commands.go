package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) commandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	switch c.Type {
	case "completeDeadline":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		var claim string
		var version int
		if err := tx.QueryRow(ctx, "SELECT claim_id::text,claim_version FROM deadlines WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID).Scan(&claim, &version); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return memory.ErrNotFound
			}
			return err
		}
		if c.MemoryID != "" && c.MemoryID != claim {
			return memory.ErrInvalid
		}
		if err := activeClaim(ctx, tx, scope, claim); err != nil {
			return err
		}
		return completeDeadlineTx(ctx, tx, scope, claim, version, c.Reason)
	case "restoreMemory":
		return restoreMemoryTx(ctx, tx, scope, c.ID)
	case "undoEntityMerge":
		return s.undoEntityCommandTx(ctx, tx, scope, c.ID)
	case "undoAction":
		return s.undoActionTx(ctx, tx, scope, c.ID)
	case "delegateTask":
		// One explicit delegation atomically creates the work and queues its run.
		// Execute's request receipt fences retries; a stable item ID also fences
		// replays under a new request ID after a lost response/reload.
		if !memory.ID(c.ID).Valid() || requireText(c.Prompt) != nil {
			return memory.ErrInvalid
		}
		agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.AgentID)
		if err != nil || !agent.Enabled || agent.Channel == "manual" || s.models == nil || !s.models.Available(agent.ID) {
			return memory.ErrUnavailable
		}
		create := c
		create.Type = "addTask"
		create.Text = c.Prompt
		if err := s.commandTx(ctx, tx, scope, create); err != nil {
			return err
		}
		run := c
		run.Type, run.ID, run.ThingID, run.Kind = "requestRun", "", c.ID, c.Kind
		if run.Kind == "" {
			run.Kind = "draft"
		}
		return s.runCommandTx(ctx, tx, scope, run)
	case "capture":
		if err := requireText(c.Text); err != nil {
			return err
		}
		in, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "capture", ExternalID: c.RequestID, ExternalVersion: "1", Title: "快速记录", Text: c.Text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		candidate := workspace.Candidate{ID: string(memory.NewID()), Kind: "unknown", Text: c.Text, Confidence: 0, Source: workspace.SourceRef{SourceID: string(in.ID), Version: in.Version, Label: "快速记录", Excerpt: c.Text, At: stamp()}, State: "pending", CreatedAt: stamp()}
		return saveCandidate(ctx, tx, scope, candidate)
	case "acceptCandidate", "ignoreCandidate", "restoreCandidate", "mergeCandidate":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		v, err := queryDocument[workspace.Candidate](ctx, tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
		if err != nil {
			return err
		}
		switch c.Type {
		case "acceptCandidate":
			if v.State != "pending" {
				return memory.ErrConflict
			}
			if err := requireText(c.Text); err != nil {
				return err
			}
			v.Kind = c.Kind
			v.Text = c.Text
			v.ProjectID = c.ProjectID
			v.MemoryKind = c.MemoryKind
			v.Due = c.Due
			if c.Kind == "memory" {
				kind := c.MemoryKind
				if kind == "" {
					kind = "fact"
				}
				ref, err := s.rememberTx(ctx, tx, scope, statement{Text: c.Text, Nature: kind, ProjectID: c.ProjectID, Source: memory.Ref{ID: memory.ID(v.Source.SourceID), Version: v.Source.Version, Kind: memory.SourceKind}})
				if err != nil {
					return err
				}
				v.ResolvedInto = string(ref.ID)
			} else if oneOf(c.Kind, "task", "idea") {
				item := newItem(c.Kind, c.Text)
				item.ProjectID = c.ProjectID
				item.Due = c.Due
				item.Sources = append(item.Sources, v.Source)
				if err := saveItem(ctx, tx, scope, item); err != nil {
					return err
				}
				v.ResolvedInto = item.ID
			} else {
				return memory.ErrInvalid
			}
			v.State = "accepted"
		case "ignoreCandidate":
			if v.State != "pending" {
				return memory.ErrConflict
			}
			v.State = "ignored"
		case "restoreCandidate":
			if v.State != "ignored" {
				return memory.ErrConflict
			}
			v.State = "pending"
		case "mergeCandidate":
			if v.State != "pending" {
				return memory.ErrConflict
			}
			item, err := getItem(ctx, tx, scope, c.TargetID)
			if err != nil {
				return err
			}
			item.Sources = append(item.Sources, v.Source)
			if item.Kind == "task" {
				item.Notes += "\n" + v.Text
			} else {
				item.Body += "\n" + v.Text
			}
			item.Version++
			item.UpdatedAt = stamp()
			if err := saveItem(ctx, tx, scope, item); err != nil {
				return err
			}
			v.State = "merged"
			v.ResolvedInto = item.ID
		}
		return saveCandidate(ctx, tx, scope, v)
	case "addTask", "addIdea", "addProject":
		kind := map[string]string{"addTask": "task", "addIdea": "idea", "addProject": "project"}[c.Type]
		title := c.Title
		if kind == "project" {
			title = c.Name
		}
		item := newItem(kind, title)
		if original, ok := ctx.Value(secretaryCreationKey{}).(workspace.SourceRef); ok {
			item.Creation = &workspace.ItemCreation{By: "secretary", Source: &original}
			if log, ok := ctx.Value(actionLogKey{}).(actionLog); ok {
				item.Creation.ActionID = log.id
			}
			if original.SourceID != "" {
				item.Sources = append(item.Sources, original)
			}
		}
		item.History[0].By = actorFromContext(ctx)
		item.Evolution[0].By = actorFromContext(ctx)
		if ctx.Value(secretaryHistoryKey{}) != nil {
			// The secretary publishes one creation revision after all fields
			// and reminders have been applied by its internal commands.
			item.History = []workspace.Revision{}
			item.Evolution = []workspace.Revision{}
		} else if item.Creation != nil && item.Creation.By == "secretary" {
			// A companion project has its own action, outside the task's history
			// collector. saveAction publishes its single creation revision.
			item.History = []workspace.Revision{}
		}
		if kind == "task" && c.Text != "" {
			if err := requireText(c.Text); err != nil {
				return err
			}
			item.Notes = c.Text
		}
		item.ProjectID = c.ProjectID
		if kind == "task" && c.Due != "" {
			if _, err := time.Parse(time.RFC3339, c.Due); err != nil {
				return memory.ErrInvalid
			}
			item.Due = c.Due
			settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
			if err != nil {
				return err
			}
			loc, err := time.LoadLocation(settings.Timezone)
			if err != nil {
				loc = time.UTC
			}
			applyDueReminder(&item, "", loc)
		}
		id, err := uuidOrNew(c.ID)
		if err != nil {
			return err
		}
		item.ID = id
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return memory.ErrConflict
		}
		return s.saveAction(ctx, tx, scope, item, "创建")
	case "bulkStatus", "bulkDefer", "bulkMove", "bulkAccept", "bulkIgnore":
		if len(c.IDs) == 0 || len(c.IDs) > 100 {
			return memory.ErrInvalid
		}
		seen := map[string]bool{}
		for _, id := range c.IDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			next := c
			next.ID = id
			next.IDs = nil
			next.Type = map[string]string{"bulkStatus": "setTaskStatus", "bulkDefer": "deferTask", "bulkMove": "moveThing", "bulkAccept": "acceptCandidate", "bulkIgnore": "ignoreCandidate"}[c.Type]
			if c.Type == "bulkAccept" {
				v, err := queryDocument[workspace.Candidate](ctx, tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
				if err != nil {
					return err
				}
				next.Kind = v.Kind
				next.Text = v.Text
				next.MemoryKind = v.MemoryKind
				next.ProjectID = v.ProjectID
				next.Due = v.Due
			}
			if err := s.commandTx(ctx, tx, scope, next); err != nil {
				return err
			}
		}
		return nil
	case "editMemory", "confirmMemory", "deleteMemory", "setMemoryVisibility", "pinMemory":
		return s.memoryCommandTx(ctx, tx, scope, c)
	case "requestRun", "pasteRunResult", "adoptRun", "discardRun":
		return s.runCommandTx(ctx, tx, scope, c)
	case "createDoc", "updateDoc", "deleteDoc":
		var doc workspace.Doc
		if c.Type == "createDoc" {
			if c.Doc == nil {
				return memory.ErrInvalid
			}
			doc = *c.Doc
			doc.Version = 0
			doc.BasedOn = nil
			if !memory.ID(doc.ID).Valid() || doc.By != "user" || doc.RunID != "" {
				return memory.ErrInvalid
			}
			if _, err := getItem(ctx, tx, scope, doc.ThingID); err != nil {
				return err
			}
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_documents WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), doc.ID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return memory.ErrConflict
			}
			doc.CreatedAt = stamp()
		} else {
			if !memory.ID(c.ID).Valid() {
				return memory.ErrInvalid
			}
			var err error
			doc, err = queryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
			if err != nil {
				return err
			}
			if c.Type == "deleteDoc" {
				_, err := tx.Exec(ctx, "DELETE FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
				return err
			}
			base := doc.Version
			doc.BasedOn = &base
			doc.By = actorFromContext(ctx)
			if err := patchAllowed(&doc, c.Patch, "title", "body"); err != nil {
				return err
			}
		}
		if err := requireText(doc.Title); err != nil {
			return err
		}
		doc.UpdatedAt = stamp()
		return saveDoc(ctx, tx, scope, doc)
	case "updateSettings":
		var fields map[string]json.RawMessage
		if json.Unmarshal(c.Patch, &fields) == nil {
			if raw, present := fields["timezone"]; present {
				var zone string
				if json.Unmarshal(raw, &zone) != nil || workspace.ValidateTimezone(zone) != nil {
					return workspace.ErrTimezone
				}
			}
		}
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		if json.Unmarshal(c.Patch, &fields) != nil || len(fields) == 0 {
			return memory.ErrInvalid
		}
		delete(fields, "autoAccept")
		c.Patch = asJSON(fields)
		if len(fields) != 0 {
			if err := patchAllowed(&settings, c.Patch, "wakeIdeas", "followUps", "dailyReviewAt", "dailyBudget", "timezone", "city"); err != nil {
				return err
			}
		}
		settings.AutoAccept = false
		settings.City = strings.TrimSpace(settings.City)
		if utf8.RuneCountInString(settings.City) > 60 || strings.ContainsAny(settings.City, "\r\n") {
			return memory.ErrInvalid
		}
		if settings.DailyBudget < 0 || settings.DailyBudget > 1e6 {
			return memory.ErrInvalid
		}
		if _, err := time.Parse("15:04", settings.DailyReviewAt); err != nil {
			return memory.ErrInvalid
		}
		if err := workspace.ValidateTimezone(settings.Timezone); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET settings=$2 WHERE owner_id=$1", string(scope.OwnerID), asJSON(settings))
		if err != nil {
			return err
		}
		if _, zone := fields["timezone"]; zone {
			return refreshTaskPlansTx(ctx, tx, scope)
		}
		if _, clock := fields["dailyReviewAt"]; clock {
			return refreshTaskPlansTx(ctx, tx, scope)
		}
		return nil
	case "updateAgent":
		a, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
		if err != nil {
			return err
		}
		if err := patchAllowed(&a, c.Patch, "enabled", "memoryKinds", "includeInferred", "note", "name"); err != nil {
			return err
		}
		if err := requireText(a.Name); err != nil {
			return err
		}
		for _, kind := range a.MemoryKinds {
			if !oneOf(kind, "fact", "preference", "decision", "intention", "plan") {
				return memory.ErrInvalid
			}
		}
		if a.Enabled && !a.MemoryInitialized {
			if err := initializeAgentMemoriesTx(ctx, tx, scope.OwnerID, a.ID); err != nil {
				return err
			}
			a.MemoryInitialized = true
		}

		_, err = tx.Exec(ctx, "UPDATE workspace_agents SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID, asJSON(a))
		return err
	case "toggleContextMemory":
		if _, err := getItem(ctx, tx, scope, c.ThingID); err != nil {
			return err
		}
		if !memory.ID(c.MemoryID).Valid() {
			return memory.ErrInvalid
		}
		if err := activeClaim(ctx, tx, scope, c.MemoryID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2 AND memory_id=$3", string(scope.OwnerID), c.ThingID, c.MemoryID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			_, err = tx.Exec(ctx, "INSERT INTO context_exclusions(owner_id,thing_id,memory_id) VALUES($1,$2,$3)", string(scope.OwnerID), c.ThingID, c.MemoryID)
		}
		return err
	case "setSampleState":
		if !memory.ID(c.ID).Valid() || !oneOf(c.State, "included", "excluded", "candidate") {
			return memory.ErrInvalid
		}
		tag, err := tx.Exec(ctx, "UPDATE training_samples SET state=$3 WHERE owner_id=$1 AND id=$2 AND (NOT stale OR $3!='included')", string(scope.OwnerID), c.ID, c.State)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return memory.ErrConflict
		}
		return nil
	case "retryJob":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		tag, err := tx.Exec(ctx, "UPDATE memory_jobs SET state='queued',attempts=0,error_code='',available_at=now(),updated_at=now() WHERE owner_id=$1 AND id=$2 AND state IN ('failed','blocked')", string(scope.OwnerID), c.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return memory.ErrConflict
		}
		return nil
	case "deleteThing":
		return s.deleteThingTx(ctx, tx, scope, c.ID)
	}
	// Remaining commands operate on a single action record.
	id := c.ID
	if c.TaskID != "" {
		id = c.TaskID
	}
	if c.IdeaID != "" {
		id = c.IdeaID
	}
	item, err := getItem(ctx, tx, scope, id)
	if err != nil {
		return err
	}
	if oneOf(c.Type, "updateTask", "setTaskStatus", "toggleTrigger", "addCheck", "toggleCheck", "removeCheck", "deferTask") && item.Kind != "task" {
		return memory.ErrInvalid
	}
	if (strings.HasPrefix(c.Type, "idea") || oneOf(c.Type, "addCondition", "removeCondition")) && item.Kind != "idea" {
		return memory.ErrInvalid
	}
	summary := c.Summary
	switch c.Type {
	case "updateTask":
		if err := patchAllowed(&item, c.Patch, "title", "notes", "status", "projectId", "due", "dueDateOnly", "scheduled", "waitingFor", "owedTo", "urgent", "dependsOn", "estimatedHours", "remindersOn"); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(c.Patch, &fields) == nil {
			if _, changed := fields["estimatedHours"]; changed {
				item.EffortSource = "user"
				item.EffortReason = "用户设定"
			}
			if _, changed := fields["due"]; changed {
				if _, known := fields["dueDateOnly"]; !known {
					value := false
					item.DueDateOnly = &value
				}
				settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
				if err != nil {
					return err
				}
				loc, err := time.LoadLocation(settings.Timezone)
				if err != nil {
					loc = time.UTC
				}
				applyDueReminder(&item, "", loc)
			}
		}
	case "updateProject":
		if item.Kind != "project" {
			return memory.ErrInvalid
		}
		if err := patchAllowed(&item, c.Patch, "name", "goal", "status"); err != nil {
			return err
		}
		item.Title = item.Name
	case "setTaskStatus":
		item.Status = c.Status
		summary = "状态改为 " + c.Status
	case "renameThing":
		item.Title = c.Title
		if item.Kind == "project" {
			item.Name = c.Title
		}
		summary = "更改名称"
	case "setNotes":
		if item.Kind == "task" {
			item.Notes = c.Text
		} else if item.Kind == "idea" {
			item.Body = c.Text
		} else {
			item.Goal = c.Text
		}
		summary = "更新说明"
	case "moveThing":
		item.ProjectID = c.ProjectID
		summary = "移动项目"
	case "deferTask":
		if c.Days < 1 || c.Days > 3650 {
			return memory.ErrInvalid
		}
		item.Scheduled = time.Now().AddDate(0, 0, c.Days).UTC().Format(time.RFC3339)
		summary = "推迟处理"
	case "toggleTrigger":
		found := false
		for i := range item.Triggers {
			if item.Triggers[i].ID == c.TriggerID {
				item.Triggers[i].Active = !item.Triggers[i].Active
				found = true
			}
		}
		if !found {
			return memory.ErrNotFound
		}
		summary = "调整提醒"
	case "addCheck":
		if err := requireText(c.Text); err != nil {
			return err
		}
		item.Checklist = append(item.Checklist, workspace.Check{ID: string(memory.NewID()), Text: c.Text})
		summary = "添加子步骤"
	case "toggleCheck", "removeCheck":
		found := false
		for i := range item.Checklist {
			if item.Checklist[i].ID == c.ItemID {
				found = true
				if c.Type == "removeCheck" {
					item.Checklist = append(item.Checklist[:i], item.Checklist[i+1:]...)
				} else {
					item.Checklist[i].Done = !item.Checklist[i].Done
				}
				break
			}
		}
		if !found {
			return memory.ErrNotFound
		}
		summary = "更新子步骤"
	case "ideaContinue":
		item.Status = "active"
		item.Wake = nil
		summary = "继续考虑"
	case "ideaPromote":
		if item.Status == "promoted" {
			return memory.ErrConflict
		}
		task := newItem("task", item.Title)
		task.Notes = item.Body
		task.ProjectID = item.ProjectID
		task.IdeaID = item.ID
		task.Sources = item.Sources
		if err := saveItem(ctx, tx, scope, task); err != nil {
			return err
		}
		if err := promoteArtifactsTx(ctx, tx, scope, item.ID, task.ID); err != nil {
			return err
		}
		item.Status = "promoted"
		item.Wake = nil
		summary = "转为待办"
	case "ideaSnooze":
		if c.Days < 1 || c.Days > 3650 {
			return memory.ErrInvalid
		}
		if item.Wake == nil {
			return memory.ErrConflict
		}
		item.Wake.SnoozedUntil = time.Now().AddDate(0, 0, c.Days).UTC().Format(time.RFC3339)
		summary = "暂缓提醒"
	case "ideaStopReminders":
		item.RemindersOn = false
		summary = "停止提醒"
	case "ideaShelve":
		item.Status = "shelved"
		item.ShelvedReason = c.Reason
		item.Wake = nil
		if strings.TrimSpace(c.Condition) != "" {
			item.Conditions = append(item.Conditions, workspace.Condition{ID: string(memory.NewID()), Kind: "event", Description: c.Condition})
		}
		summary = "搁置：" + c.Reason
	case "ideaDrop":
		item.Status = "dropped"
		item.RemindersOn = false
		item.Wake = nil
		summary = "放下想法"
	case "ideaNote":
		if err := requireText(c.Note); err != nil {
			return err
		}
		item.Body += "\n" + c.Note
		summary = c.Note
	case "addCondition":
		if err := requireText(c.Description); err != nil {
			return err
		}
		kind := "event"
		if c.Due != "" {
			if _, err := time.Parse(time.RFC3339, c.Due); err != nil {
				return memory.ErrInvalid
			}
			kind = "time"
		}
		item.Conditions = append(item.Conditions, workspace.Condition{ID: string(memory.NewID()), Kind: kind, Description: c.Description, DueAt: c.Due})
		summary = "新增唤醒条件"
	case "removeCondition":
		found := false
		for i := range item.Conditions {
			if item.Conditions[i].ID == c.ConditionID {
				item.Conditions = append(item.Conditions[:i], item.Conditions[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return memory.ErrNotFound
		}
		summary = "移除唤醒条件"
	default:
		return memory.ErrInvalid
	}
	item.Version++
	item.UpdatedAt = stamp()
	if summary == "" {
		summary = "更新"
	}
	return s.saveAction(ctx, tx, scope, item, summary)
}
func saveCandidate(ctx context.Context, tx pgx.Tx, scope memory.Scope, v workspace.Candidate) error {
	_, err := tx.Exec(ctx, `INSERT INTO capture_candidates(owner_id,id,source_id,source_version,state,document) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(owner_id,id) DO UPDATE SET state=excluded.state,document=excluded.document`, string(scope.OwnerID), v.ID, v.Source.SourceID, v.Source.Version, v.State, asJSON(v))
	return err
}
func saveDoc(ctx context.Context, tx pgx.Tx, scope memory.Scope, doc workspace.Doc) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_documents WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), doc.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		_, err := tx.Exec(ctx, "UPDATE work_documents SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), doc.ID, asJSON(doc))
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO work_documents(owner_id,id,thing_id,document) VALUES($1,$2,$3,$4)", string(scope.OwnerID), doc.ID, doc.ThingID, asJSON(doc))
	return err
}
func (s *Store) saveAction(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, summary string) error {
	if history, ok := ctx.Value(secretaryHistoryKey{}).(*secretaryHistory); ok {
		history.record(item.ID, summary)
		return saveItem(ctx, tx, scope, item)
	}
	revision := workspace.Revision{At: stamp(), By: actorFromContext(ctx), Summary: summary}
	item.History = append(item.History, revision)
	if item.Kind == "idea" {
		item.Evolution = append(item.Evolution, revision)
	}
	if smokeID(ctx) != "" {
		return saveItem(ctx, tx, scope, item)
	}
	// Action records remain authoritative for execution; memory retains a versioned receipt.
	source, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "actions", ExternalID: item.ID, ExternalVersion: fmt.Sprint(item.Version), Title: item.Title, Text: string(asJSON(item)), MediaType: "text/plain"})
	if err != nil {
		return err
	}
	item.Sources = append(item.Sources, workspace.SourceRef{SourceID: string(source.ID), Version: source.Version, Label: summary, At: revision.At})
	return saveItem(ctx, tx, scope, item)
}

func activeClaim(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_records WHERE owner_id=$1 AND id=$2 AND kind='claim' AND state='active')", string(scope.OwnerID), id).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return memory.ErrNotFound
	}
	return nil
}
func sampleTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, sample workspace.Sample) error {
	_, err := tx.Exec(ctx, "INSERT INTO training_samples(owner_id,id,run_id,memory_id,state,stale,document) VALUES($1,$2,$3,$4,$5,$6,$7)", string(scope.OwnerID), sample.ID, nullString(sample.Origin.RunID), nullString(sample.Origin.MemoryID), sample.State, sample.Stale, asJSON(sample))
	return err
}

// The wire format is deliberately a small explicit command vocabulary. Client
// supplied run outputs, costs, provenance, and processing states are not trusted.

// deleteThingTx removes a to-do, idea or empty project for good: its
// reminders, documents and agent runs go with it, other to-dos stop depending
// on it, and the receipts memory kept of its changes are deleted like any
// other material. It is confirmed in the interface and cannot be undone.
func (s *Store) deleteThingTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	item, err := getItem(ctx, tx, scope, id)
	if err != nil {
		return err
	}
	var children, running bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND project_id=$2),
		EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 AND status='running')`, string(scope.OwnerID), item.ID).Scan(&children, &running); err != nil {
		return err
	}
	if children {
		return memory.ErrConflict
	}
	if running {
		return workspace.ErrWorkStarted
	}
	var receipt memory.Ref
	err = tx.QueryRow(ctx, `SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
		WHERE s.owner_id=$1 AND s.connector='actions' AND s.external_id=$2 AND r.state='active'`, string(scope.OwnerID), item.ID).Scan(&receipt.ID, &receipt.Version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		receipt.Kind = memory.SourceKind
		if err := s.deleteRecordsTx(ctx, tx, scope, memory.DeleteRequest{Targets: []memory.Ref{receipt}}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE work_items SET document=jsonb_set(document,'{dependsOn}',(document->'dependsOn')-$2)
		WHERE owner_id=$1 AND kind='task' AND document->'dependsOn' ? $2`, string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	// Undo keeps earlier versions of the item, its documents, its agent runs
	// and the samples drawn from them. A deletion empties those too: the
	// trigger does it for the item and its documents once this setting names
	// the owner, and the runs and samples are named here.
	var previous string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.expire_actions',true),'')").Scan(&previous); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", string(scope.OwnerID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE action_log SET changes='[]'::jsonb,expired_at=now()
		WHERE owner_id=$1 AND expired_at IS NULL AND EXISTS(SELECT 1 FROM jsonb_array_elements(changes) c WHERE
		 (c->>'table'='agent_runs' AND c->>'id' IN (SELECT id::text FROM agent_runs WHERE owner_id=$1 AND thing_id=$2))
		 OR (c->>'table'='training_samples' AND c->>'id' IN (SELECT t.id::text FROM training_samples t JOIN agent_runs r ON (r.owner_id,r.id)=(t.owner_id,t.run_id) WHERE r.owner_id=$1 AND r.thing_id=$2))
		 OR (c->>'table'='work_items' AND c->>'id'=$2::text))`, string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	// Samples drawn from this item's agent runs refer to those runs and go first.
	if _, err := tx.Exec(ctx, "DELETE FROM training_samples WHERE owner_id=$1 AND run_id IN (SELECT id FROM agent_runs WHERE owner_id=$1 AND thing_id=$2)", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", previous)
	return err
}
