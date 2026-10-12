package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The table of deadlines holds every date a memory mentions. Most of them are
// not things to be shown or done: a trip that is over, a term someone quoted,
// a habit once considered. A secretary sorts these without being asked. This
// stage judges each such date once per memory version and acts — keep it, stop
// showing it, or make it the to-do or idea it really is — with an undoable
// action for everything it changes.
const DateTidyStage = datereview.Stage

// One small call per date. Thirty an hour clears a backlog of a few hundred
// rows within a day without holding the provider; a normal day adds a handful.
// Jobs past the hour's budget wait for the next hour; none is dropped.
const dateTidyHourly = 30

// Open to-dos and ideas shown so the model does not make a second copy. The
// secretary is shown the same 40 and 20 (2.6 C6); what is left out is counted.
const dateTidyTaskTitles = 40
const dateTidyIdeaTitles = 20
const dateTidyTextCharacters = 1200

// Other open rows of the table with the same title, so one thing read twice
// is shown once. The newest five; a title repeated more often than that is a
// fixed arrangement, which has its own rule.
const dateTidySameTitles = 5
const dateTidySameCharacters = 300

// A dated row is left alone until its day has been over for this long, so
// something due last night is still the user's to tick on the home page.
const dateTidyPastAfter = 24 * time.Hour

var dateTidyInstructions = prompts.Must("date-tidy").Text()

// The input assembly below is the context builder of the date review service.
type dateTidyEntry = datereview.Entry
type dateTidyInput = datereview.Input
type dateTidySame = datereview.Same

// dateTidyEligible lists the dates that are open and are not a time to show:
// never pinned down, a fixed arrangement with no hour, or over for a day. A
// row already judged for this memory version and class is not listed again.
const dateTidyEligible = `SELECT DISTINCT ON(d.claim_id) d.claim_id::text,d.claim_version,
 CASE WHEN d.kind='unclear' THEN 'unclear' WHEN d.kind='recurring' THEN 'habit' ELSE 'past' END AS class,
 d.kind,d.title,coalesce(to_char(d.at AT TIME ZONE $3,'YYYY-MM-DD HH24:MI'),''),d.recurrence,d.time_note,d.original_text,
 c.value #>> '{}',coalesce(to_char(rv.expressed_at AT TIME ZONE $3,'YYYY-MM-DD'),''),coalesce(c.acquisition,'')
 FROM deadlines d
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(d.owner_id,d.claim_id)
 LEFT JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(d.owner_id,d.claim_id,d.claim_version)
 WHERE d.owner_id=$1 AND r.state='active' AND cl.retired=''
 AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text)
 AND (d.kind='unclear' OR (d.kind='recurring' AND coalesce(d.schedule_rule->>'clock','')='') OR (d.kind IN('deadline','appointment') AND d.at<$2))
 AND ($4::uuid IS NULL OR d.claim_id=$4)
 AND NOT EXISTS(SELECT 1 FROM date_tidy_checks k WHERE(k.owner_id,k.claim_id,k.claim_version)=(d.owner_id,d.claim_id,d.claim_version)
  AND k.class=CASE WHEN d.kind='unclear' THEN 'unclear' WHEN d.kind='recurring' THEN 'habit' ELSE 'past' END)
 ORDER BY d.claim_id,d.id`

type dateTidyRow struct {
	Claim   string
	Version int
	Class   string
	Entry   dateTidyEntry
}

func dateTidyRowsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, now time.Time, claim *string) ([]dateTidyRow, string, error) {
	var timezone string
	if err := tx.QueryRow(ctx, "SELECT coalesce(nullif(settings->>'timezone',''),'UTC') FROM workspace_owners WHERE owner_id=$1", string(owner)).Scan(&timezone); errors.Is(err, pgx.ErrNoRows) {
		timezone = "UTC"
	} else if err != nil {
		return nil, "", err
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		timezone = "UTC"
	}
	rows, err := tx.Query(ctx, dateTidyEligible, string(owner), now.Add(-dateTidyPastAfter), timezone, claim)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []dateTidyRow
	for rows.Next() {
		var r dateTidyRow
		if err = rows.Scan(&r.Claim, &r.Version, &r.Class, &r.Entry.Kind, &r.Entry.Title, &r.Entry.At, &r.Entry.Recurrence, &r.Entry.TimeNote, &r.Entry.OriginalText, &r.Entry.Memory, &r.Entry.SaidOn, &r.Entry.Acquisition); err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	return out, timezone, rows.Err()
}

// enqueueDateTidyTx queues one judgement per eligible date. The job is keyed by
// memory version and class, so an edit to the memory, or a dated row passing
// into the past, is judged afresh and nothing else is redone.
func enqueueDateTidyTx(ctx context.Context, tx pgx.Tx, owner memory.ID, now time.Time) (int, error) {
	var installed bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('date_tidy_checks') IS NOT NULL").Scan(&installed); err != nil || !installed {
		return 0, err
	}
	rows, _, err := dateTidyRowsTx(ctx, tx, owner, now, nil)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range rows {
		tag, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at) VALUES($1,$2,$3,$4,$5,9,$6)
 ON CONFLICT(owner_id,record_id,record_version,stage) DO NOTHING`, string(memory.NewID()), string(owner), r.Claim, r.Version, DateTidyStage+":"+r.Class, now)
		if err != nil {
			return count, err
		}
		count += int(tag.RowsAffected())
	}
	return count, nil
}

func clipRunes(text string, limit int) (string, int) {
	r := []rune(text)
	if len(r) <= limit {
		return text, 0
	}
	return string(r[:limit]), len(r) - limit
}

func dateTidyInputTx(ctx context.Context, tx pgx.Tx, j worker.Job, class string, now time.Time) (dateTidyInput, bool, int, error) {
	claim := string(j.Record.ID)
	rows, timezone, err := dateTidyRowsTx(ctx, tx, j.OwnerID, now, &claim)
	if err != nil {
		return dateTidyInput{}, false, 0, err
	}
	if len(rows) != 1 || rows[0].Version != j.Record.Version || rows[0].Class != class {
		return dateTidyInput{}, false, 0, nil
	}
	loc, _ := time.LoadLocation(timezone)
	in := dateTidyInput{Now: now.In(loc).Format("2006-01-02 15:04"), Timezone: timezone, Class: class, Entry: rows[0].Entry, SameTitle: []dateTidySame{}, OpenTasks: []string{}, Ideas: []string{}}
	left := 0
	var cut int
	in.Entry.OriginalText, cut = clipRunes(in.Entry.OriginalText, dateTidyTextCharacters)
	left += cut
	in.Entry.Memory, cut = clipRunes(in.Entry.Memory, dateTidyTextCharacters)
	left += cut
	if in.SameTitle, err = kernel.QueryDocuments[dateTidySame](ctx, tx, `SELECT jsonb_build_object('at',coalesce(to_char(d.at AT TIME ZONE $4,'YYYY-MM-DD HH24:MI'),''),'saidOn',coalesce(to_char(rv.expressed_at AT TIME ZONE $4,'YYYY-MM-DD'),''),'originalText',left(d.original_text,$5))
 FROM deadlines d
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(d.owner_id,d.claim_id)
 LEFT JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(d.owner_id,d.claim_id,d.claim_version)
 WHERE d.owner_id=$1 AND r.state='active' AND cl.retired='' AND d.claim_id<>$3
 AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text)
 AND lower(btrim(d.title))=lower(btrim($2))
 ORDER BY rv.expressed_at DESC NULLS LAST,d.claim_id,d.id LIMIT $6`, string(j.OwnerID), in.Entry.Title, claim, timezone, dateTidySameCharacters, dateTidySameTitles); err != nil {
		return in, false, 0, err
	}
	titles := func(kind, where string, limit int) ([]string, error) {
		return kernel.QueryDocuments[string](ctx, tx, "SELECT to_jsonb(document->>'title') FROM work_items WHERE owner_id=$1 AND kind=$2 AND "+where+" ORDER BY updated_at DESC,id LIMIT $3", string(j.OwnerID), kind, limit)
	}
	if in.OpenTasks, err = titles("task", "status IN('todo','doing','waiting')", dateTidyTaskTitles); err != nil {
		return in, false, 0, err
	}
	if in.Ideas, err = titles("idea", "status NOT IN('done','cancelled','dropped')", dateTidyIdeaTitles); err != nil {
		return in, false, 0, err
	}
	return in, true, left, nil
}

// DateReviewStore gives the date review service its reads and writes.
func (s *Store) DateReviewStore() datereview.Store { return dateReviewStore{s} }

type dateReviewStore struct{ store *Store }

func (a dateReviewStore) Input(ctx context.Context, j worker.Job, class string) (input datereview.Input, open bool, clipped int, err error) {
	now := a.store.BusinessNow()
	err = pgx.BeginTxFunc(ctx, a.store.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		input, open, clipped, e = dateTidyInputTx(ctx, tx, j, class, now)
		return e
	})
	return input, open, clipped, err
}

func (a dateReviewStore) Drop(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, a.store.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		if err := kernel.DiscardPaidResult(ctx, tx, j); err != nil {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

func (a dateReviewStore) DiscardResult(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, a.store.Pool, func(tx pgx.Tx) error { return kernel.DiscardPaidResult(ctx, tx, j) })
}

func (a dateReviewStore) RetryLater(j worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(kernel.RetryDelay(j.Attempts))}
}

func (a dateReviewStore) Apply(ctx context.Context, j worker.Job, fn func(datereview.Unit) error) error {
	now := a.store.BusinessNow()
	return kernel.BackgroundResultTx(ctx, a.store.Pool, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(dateReviewUnit{ctx: ctx, tx: tx, job: j, now: now, scope: memory.Scope{OwnerID: j.OwnerID, PrincipalID: "owner", IsOwner: true}})
	})
}

type dateReviewUnit struct {
	ctx   context.Context
	tx    pgx.Tx
	job   worker.Job
	now   time.Time
	scope memory.Scope
}

func (u dateReviewUnit) Input(class string) (datereview.Input, bool, error) {
	input, open, _, err := dateTidyInputTx(u.ctx, u.tx, u.job, class, u.now)
	return input, open, err
}

func (u dateReviewUnit) Event(outcome, reason string, count int) error {
	return kernel.StageEvent(u.ctx, u.tx, u.job.OwnerID, DateTidyStage, outcome, reason, count)
}

func (u dateReviewUnit) Act(label string, fn func(datereview.Action) error) (string, error) {
	id := string(memory.NewID())
	actionCtx := withActionLog(kernel.WithActor(u.ctx, "ai"), id, "background_tidy", "", label)
	if err := beginActionLogTx(actionCtx, u.tx); err != nil {
		return "", err
	}
	if err := fn(dateReviewAction{ctx: actionCtx, unit: u, id: id}); err != nil {
		return "", err
	}
	return id, flushActionLog(actionCtx, u.tx, u.scope)
}

func (u dateReviewUnit) RecordDecision(class string, input datereview.Input, output datereview.Output, itemID, actionID *string) error {
	_, err := u.tx.Exec(u.ctx, `INSERT INTO date_tidy_checks(owner_id,claim_id,claim_version,class,decision,reason,entry_title,title,item_id,action_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		string(u.job.OwnerID), string(u.job.Record.ID), u.job.Record.Version, class, output.Decision, strings.TrimSpace(output.Reason), input.Entry.Title, output.Title, itemID, actionID)
	return err
}

func (u dateReviewUnit) DiscardResult() error { return kernel.DiscardPaidResult(u.ctx, u.tx, u.job) }
func (u dateReviewUnit) Acknowledge() error   { return kernel.Acknowledge(u.ctx, u.tx, u.job) }

type dateReviewAction struct {
	ctx  context.Context
	unit dateReviewUnit
	id   string
}

func (a dateReviewAction) ID() string { return a.id }
func (a dateReviewAction) SaveItem(item workspace.Item) error {
	return saveItem(a.ctx, a.unit.tx, a.unit.scope, item)
}
func (a dateReviewAction) CloseDate(as string) error {
	return completeDeadlineTx(a.ctx, a.unit.tx, a.unit.scope, string(a.unit.job.Record.ID), a.unit.job.Record.Version, as)
}

// tidiedDatesTx is the home page's one line for the dates the background
// stopped showing in the last two days — as long as anything else stays under
// "while you were away". Each date is listed under it with why, and its own
// undo. To-dos and ideas made from a date have their receipt on the item.
func tidiedDatesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (*workspace.Activity, error) {
	var installed bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('date_tidy_checks') IS NOT NULL").Scan(&installed); err != nil || !installed {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT a.id::text,a.created_at,k.entry_title,k.reason FROM date_tidy_checks k
 JOIN action_log a ON(a.owner_id,a.id)=(k.owner_id,k.action_id)
 WHERE k.owner_id=$1 AND k.decision='drop' AND a.undone_at IS NULL AND a.expired_at IS NULL AND a.created_at>=now()-interval '2 days'
 ORDER BY a.created_at DESC,a.id`, string(scope.OwnerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := workspace.Activity{ID: "date-tidy"}
	for rows.Next() {
		var item workspace.ActivityItem
		var at time.Time
		if err = rows.Scan(&item.ActionID, &at, &item.Text, &item.Note); err != nil {
			return nil, err
		}
		if out.At == "" {
			out.At = at.UTC().Format(time.RFC3339Nano)
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil || len(out.Items) == 0 {
		return nil, err
	}
	out.Text = fmt.Sprintf("收拾了日程：%d 条过了期或不作数的，不再显示", len(out.Items))
	return &out, nil
}
