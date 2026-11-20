// Package topicproject owns the workspace rule that turns a group of memories
// about one topic into a project, or attaches it to an existing project.
// The model proposes. This package validates the proposal and decides what is
// written. Storage supplies the input and the transactions.
package topicproject

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Stage is the queue stage. One job's stage is
// "memory.topic_project:<topic id>:<input hash>:<project page>".
const Stage = "memory.topic_project"

// HourlyCalls is the number of topic project checks that can reserve a model call in one
// hour. Unused capacity is not lent to another stage.
const HourlyCalls = 2

const (
	// DailyLimit is the number of projects that this path creates in one local day.
	DailyLimit = 4
	// PageSize is the number of existing projects that one call compares against.
	PageSize = 20
	// GoalCharacters is the longest goal that a created project keeps.
	GoalCharacters = 400
)

// Input is one topic's current evidence and the owner's projects. Its JSON form
// is the model input; fields without a JSON name stay in the program.
type Input struct {
	ClippedCharacters int              `json:"-"`
	EvidenceHash      string           `json:"-"`
	Topic             string           `json:"topic"`
	Kind              string           `json:"kind"`
	Now               string           `json:"now"`
	Key               string           `json:"key"`
	Hash              string           `json:"-"`
	Latest            time.Time        `json:"-"`
	Version           int              `json:"-"`
	Count             int              `json:"memoryCount"`
	Memories          []map[string]any `json:"memories"`
	Projects          []Summary        `json:"projects"`
	Timezone          string           `json:"timezone"`
}

type Summary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Goal   string `json:"goal"`
	Status string `json:"status"`
}

// Output is the model's proposal for one page of projects.
type Output struct {
	Decision  string  `json:"decision"`
	ProjectID *string `json:"projectId"`
	Name      string  `json:"name"`
	Goal      string  `json:"goal"`
	Reason    string  `json:"reason"`
}

// Store reads and writes the records of one topic project job.
type Store interface {
	// Input assembles the topic's current input. found is false when the topic
	// no longer qualifies.
	Input(ctx context.Context, owner memory.ID, topic string) (input Input, refs []memory.Ref, found bool, err error)
	// Drop discards the job's saved result and acknowledges the job.
	Drop(ctx context.Context, job worker.Job) error
	DiscardResult(ctx context.Context, job worker.Job) error
	// RetryLater returns the queue error that tries the job again after the queue's delay.
	RetryLater(job worker.Job, code string) error
	// Apply runs fn in one transaction that holds the owner and the job's lease.
	Apply(ctx context.Context, job worker.Job, fn func(Unit) error) error
}

// Unit is the set of reads and writes that one Apply transaction permits.
type Unit interface {
	Input(topic string) (input Input, found bool, err error)
	RecordCheck(topic string, input Input, page int, output Output) error
	EnqueuePage(topic string, input Input, page int) error
	ProjectByName(name string) (id string, found bool, err error)
	Project(id string) (workspace.Item, error)
	ProjectsCreatedSince(at time.Time) (int, error)
	SaveProject(workspace.Item) error
	LinkTopic(topic string, input Input, project, actionID string, created bool) error
	LogAction(actionID, summary string) error
	DiscardResult() error
	Acknowledge() error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

type Service struct {
	Store     Store
	Generator Generator
	// Now supplies business time for the model input.
	Now func() time.Time
}

// Process handles one topic project job.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	parts := strings.Split(job.Stage, ":")
	if len(parts) != 4 {
		return memory.ErrInvalid
	}
	topic := parts[1]
	page, err := strconv.Atoi(parts[3])
	if err != nil || page < 0 || !memory.ID(topic).Valid() {
		return memory.ErrInvalid
	}
	input, refs, found, err := s.Store.Input(ctx, job.OwnerID, topic)
	if err != nil {
		return err
	}
	// The job was made for other evidence. Its answer is no longer wanted.
	if !found || input.Hash != parts[2] {
		return s.Store.Drop(ctx, job)
	}
	start := page * PageSize
	if start > len(input.Projects) {
		return memory.ErrInvalid
	}
	end := min(start+PageSize, len(input.Projects))
	promptInput := input
	promptInput.Projects = input.Projects[start:end]
	if loc, e := time.LoadLocation(input.Timezone); e == nil {
		promptInput.Now = s.Now().In(loc).Format("2006-01-02")
	} else {
		promptInput.Now = s.Now().UTC().Format("2006-01-02")
	}
	record, _ := json.Marshal(promptInput)
	_, text, err := s.Generator.GenerateForJob(ctx, job, "topic_project", prompts.Must("topic-project"), "", record, refs)
	if err != nil {
		return err
	}
	output, valid := Validate(text, input, promptInput.Projects)
	if !valid || output.Decision == "uncertain" {
		if err := s.Store.DiscardResult(ctx, job); err != nil {
			return err
		}
		code := "topic_project_output_invalid"
		if valid {
			code = "topic_project_uncertain"
		}
		return s.Store.RetryLater(job, code)
	}
	return s.Store.Apply(ctx, job, func(u Unit) error {
		return apply(u, topic, input, refs, output, page, end < len(input.Projects))
	})
}

// Validate reads a proposal and applies the rules that do not need storage.
// A match must name a project that the model saw. Other decisions name none.
// What the user calls a project keeps the user's name for it.
func Validate(text string, input Input, shown []Summary) (Output, bool) {
	var output Output
	valid := modelcall.DecodeStrict([]byte(strings.TrimSpace(text)), &output) == nil && (output.Decision == "match" || output.Decision == "new" || output.Decision == "skip" || output.Decision == "uncertain") && strings.TrimSpace(output.Reason) != ""
	if output.Decision == "match" {
		found := false
		if output.ProjectID != nil {
			for _, p := range shown {
				found = found || p.ID == *output.ProjectID
			}
		}
		valid = valid && found
	} else {
		valid = valid && output.ProjectID == nil
	}
	if output.Decision == "new" && input.Kind == "project" && workspace.ValidTitle(input.Topic) {
		output.Name = input.Topic
	}
	if output.Decision == "new" {
		valid = valid && workspace.ValidTitle(output.Name)
	}
	return output, valid
}

func apply(u Unit, topic string, input Input, refs []memory.Ref, output Output, page int, morePages bool) error {
	fresh, found, err := u.Input(topic)
	if err != nil {
		return err
	}
	// The evidence changed during the call. The scheduler makes a new job.
	if !found || fresh.Hash != input.Hash {
		if err := u.DiscardResult(); err != nil {
			return err
		}
		return u.Acknowledge()
	}
	if err := u.RecordCheck(topic, input, page, output); err != nil {
		return err
	}
	finish := func() error {
		if err := u.DiscardResult(); err != nil {
			return err
		}
		return u.Acknowledge()
	}
	// Not a project: the verdict is kept with the evidence it was given on,
	// so the group is asked about again only when what is said of it changes.
	if output.Decision == "skip" {
		return finish()
	}
	// A new project is created only after the model has seen every page of
	// existing projects.
	if output.Decision == "new" && morePages {
		if err := u.EnqueuePage(topic, input, page+1); err != nil {
			return err
		}
		return finish()
	}
	created := output.Decision == "new"
	project := ""
	if output.ProjectID != nil {
		project = *output.ProjectID
	}
	if created {
		// Same-name equality is only a uniqueness guard, after model matching all pages.
		if id, found, err := u.ProjectByName(strings.TrimSpace(output.Name)); err != nil {
			return err
		} else if found {
			project, created = id, false
		}
	}
	actionID := string(memory.NewID())
	summary, name := "归入已有项目「", output.Name
	if created {
		summary = "建了项目「"
	} else {
		existing, err := u.Project(project)
		if err != nil {
			return err
		}
		name = existing.Title
	}
	if created {
		loc, err := time.LoadLocation(input.Timezone)
		if err != nil {
			return err
		}
		now := time.Now().In(loc)
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		count, err := u.ProjectsCreatedSince(midnight)
		if err != nil {
			return err
		}
		if count >= DailyLimit {
			return &worker.JobError{Code: "topic_project_daily_limit", Until: midnight.AddDate(0, 0, 1), NoAttempt: true}
		}
		item := workspace.NewItem("project", strings.TrimSpace(output.Name))
		goal := []rune(strings.TrimSpace(output.Goal))
		if len(goal) > GoalCharacters {
			goal = goal[:GoalCharacters]
		}
		item.Goal = string(goal)
		item.History[0].By = "ai"
		item.Evolution[0].By = "ai"
		project = item.ID
		ids := []string{}
		for _, ref := range refs {
			ids = append(ids, string(ref.ID))
		}
		item.Creation = &workspace.ItemCreation{By: "background_topic", ActionID: actionID, MemoryIDs: ids, GroupKey: input.Key}
		if err := u.SaveProject(item); err != nil {
			return err
		}
	}
	if err := u.LinkTopic(topic, input, project, actionID, created); err != nil {
		return err
	}
	if err := u.LogAction(actionID, summary+name+"」"); err != nil {
		return err
	}
	return finish()
}

// MemoryCharacters is the text of one memory in the model input of a topic.
const MemoryCharacters = 1200

// ClipGoal cuts a project goal to its room in the model input and says that
// it goes on. clipped is the count of characters cut.
func ClipGoal(goal string) (text string, clipped int) {
	r := []rune(goal)
	if len(r) <= GoalCharacters {
		return goal, 0
	}
	return string(r[:GoalCharacters]) + "（目标还有内容）", len(r) - GoalCharacters
}

// ClipMemory cuts the text of a memory to its room in the model input and
// tells the model to answer uncertain when the rest might matter.
func ClipMemory(text string) (string, int) {
	r := []rune(text)
	if len(r) <= MemoryCharacters {
		return text, 0
	}
	return string(r[:MemoryCharacters]) + "（原文还有内容，拿不准请用 uncertain）", len(r) - MemoryCharacters
}

// A group is judged as a project when it has this many current memories, at
// most EvidenceLimit of them go into the input, and somebody spoke of it
// within Alive. A trip in June or an exam in spring has memories, a goal and
// a date, and made a project all the same; Alive keeps them out.
const (
	Minimum       = 10
	EvidenceLimit = 10
	Alive         = 45 * 24 * time.Hour
)

// Eligible is true for a group that can be judged as a project: what the
// owner calls a project needs no date; a mere topic needs a goal and a date.
// latest is the time of its latest memory, or zero when none has a time.
func Eligible(count int, kind string, goal, date bool, latest, now time.Time) bool {
	if count < Minimum || kind == "topic" && (!goal || !date) {
		return false
	}
	return latest.IsZero() || now.Sub(latest) <= Alive
}
