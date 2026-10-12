package app

import (
	"fmt"
	"strings"

	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/interpretation/aliasscan"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
	"github.com/soaringjerry/PCAS/internal/workspace/topicproject"
)

// Response is one row of the event-response table: a trigger, the owner that
// produces it, the named specialist that responds, and the queue stage of that
// response. The worker's handler table and the documented table both come
// from these rows.
type Response struct {
	Stage      string
	Trigger    string
	Producer   string
	Specialist string
	// Duplicate says what makes two queued responses the same work.
	Duplicate string
	handler   func(*postgres.Store) worker.Handler
}

// Every queued response has the same base duplicate key. The stage of some
// responses carries a further identity, which the row names.
const jobKey = "owner, record, record version, stage"

// Responses is the one authoritative table. A stage that is not here has no
// handler, and the worker does not run it.
var Responses = []Response{
	{"source.parse", "Source version accepted, with an attachment", "Input", "Attachment processor", jobKey, func(db *postgres.Store) worker.Handler { return Attachment(db) }},
	{"source.chunk", "Source version accepted, with text", "Input", "Text processor", jobKey, func(db *postgres.Store) worker.Handler { return db.ProcessChunks }},
	{extraction.Stage, "Source text available", "Input", "Memory extraction specialist", jobKey, func(db *postgres.Store) worker.Handler { return Extraction(db).Process }},
	{"source.tokenize", "Search input changed for a source", "Input", "Search index processor", jobKey, func(db *postgres.Store) worker.Handler { return db.ProcessIndex }},
	{"memory.index", "Search input changed for a memory", "Memory", "Search index processor", jobKey, func(db *postgres.Store) worker.Handler { return db.ProcessIndex }},
	{"source.embed", "Embedding input changed for a source", "Input", "Vector index processor", jobKey, func(db *postgres.Store) worker.Handler { return Embedding(db).Process }},
	{"memory.embed", "Embedding input changed for a memory", "Memory", "Vector index processor", jobKey, func(db *postgres.Store) worker.Handler { return Embedding(db).Process }},
	{organize.Stage, "Statement interpretation input changed", "Memory", "Organization specialist", jobKey, func(db *postgres.Store) worker.Handler { return Organize(db).Process }},
	{comparison.Stage, "Statement interpretation input changed", "Memory", "Statement comparison specialist", jobKey + "; the stage names the comparison batch", func(db *postgres.Store) worker.Handler { return Compare(db).Process }},
	{aliasscan.Stage, "Entity comparison input changed", "Memory", "Entity candidate specialist", jobKey, func(db *postgres.Store) worker.Handler { return AliasScan(db).Process }},
	{comparison.EntityStage, "Entity comparison input changed", "Memory", "Entity comparison specialist", jobKey, func(db *postgres.Store) worker.Handler { return EntityCompare(db).Process }},
	{"memory.summary", "Episode summary input changed", "Memory", "Summary specialist", jobKey, func(db *postgres.Store) worker.Handler { return db.ProcessSummary }},
	{"memory.card", "Queued card work from before cards were removed", "Current state", "Queue acknowledgement only", jobKey, func(db *postgres.Store) worker.Handler { return db.ProcessCard }},
	{handover.Stage, "Scheduled freshness check: global handover input changed", "Current state", "Global handover specialist", jobKey, func(db *postgres.Store) worker.Handler { return Handover(db).Process }},
	{datereview.Stage, "Scheduled freshness check: a remembered date is not a time to show", "Current state", "Date review specialist", jobKey + "; the stage names the class of the date", func(db *postgres.Store) worker.Handler { return DateReview(db).Process }},
	{projecthandover.Stage, "Scheduled freshness check: project input changed", "Workspace", "Project handover specialist", jobKey + "; the stage names the project", func(db *postgres.Store) worker.Handler { return ProjectHandover(db).Process }},
	{effort.Stage, "Scheduled freshness check: a project has tasks without an estimate", "Workspace", "Effort specialist", jobKey + "; the stage names the project", func(db *postgres.Store) worker.Handler { return Effort(db).Process }},
	{topicproject.Stage, "Scheduled freshness check: a topic's evidence changed", "Workspace", "Project candidate specialist", jobKey + "; the stage names the topic, its input hash, and the project page", func(db *postgres.Store) worker.Handler { return TopicProject(db).Process }},
}

// Handlers gives the background worker one handler for each queue stage,
// from the response table.
func Handlers(db *postgres.Store) map[string]worker.Handler {
	handlers := make(map[string]worker.Handler, len(Responses))
	for _, r := range Responses {
		handlers[r.Stage] = r.handler(db)
	}
	return handlers
}

// ResponseTable is the documented form of the table. The architecture page
// holds this text, and a test keeps the two equal.
func ResponseTable() string {
	limits := postgres.StageHourlyLimits()
	var b strings.Builder
	b.WriteString("| Stage | Trigger | Producing owner | Named response | Calls in one hour | Duplicate key |\n|---|---|---|---|---|---|\n")
	for _, r := range Responses {
		limit := "No stage limit"
		if n, ok := limits[r.Stage]; ok {
			limit = fmt.Sprint(n)
		}
		fmt.Fprintf(&b, "| `%s` | %s. | %s. | %s. | %s | %s. |\n", r.Stage, r.Trigger, r.Producer, r.Specialist, limit, r.Duplicate)
	}
	return b.String()
}
