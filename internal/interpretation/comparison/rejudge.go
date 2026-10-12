package comparison

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// RejudgeRefs reads the superseded statement and its successor from the stage
// of a rejudge job: "memory.compare:<rule version>:rejudge:<old id>:<old
// version>:<new id>:<new version>". The job's record is the old statement.
func RejudgeRefs(job worker.Job, version int) ([]memory.Ref, error) {
	parts := strings.Split(job.Stage, ":")
	if len(parts) != 7 || parts[0] != Stage || parts[2] != "rejudge" {
		return nil, memory.ErrInvalid
	}
	rule, err := strconv.Atoi(parts[1])
	if err != nil || rule != version {
		return nil, memory.ErrInvalid
	}
	refs := []memory.Ref{}
	for i := 3; i < 7; i += 2 {
		v, err := strconv.Atoi(parts[i+1])
		id := memory.ID(parts[i])
		if err != nil || v < 1 || !id.Valid() {
			return nil, memory.ErrInvalid
		}
		refs = append(refs, memory.Ref{ID: id, Version: v, Kind: memory.ClaimKind})
	}
	if refs[0] != job.Record {
		return nil, memory.ErrInvalid
	}
	return refs, nil
}

// ParseRejudgeOutput reads the verdict on one superseded pair. A discarded
// malformed proposal is not a negative judgment. Unlike a batch comparison,
// which can ignore one edge, a rejudge requires a complete verdict.
func ParseRejudgeOutput(text string) ([]Edge, bool) {
	var e struct {
		Duplicates []json.RawMessage `json:"duplicates"`
		Superseded []json.RawMessage `json:"superseded"`
	}
	if modelcall.DecodeStrict([]byte(text), &e) != nil || e.Duplicates == nil || e.Superseded == nil || len(e.Duplicates)+len(e.Superseded) > 1 {
		return nil, false
	}
	if len(e.Superseded) == 1 {
		var p struct {
			Old int `json:"old"`
			New int `json:"new"`
		}
		if modelcall.DecodeStrict(e.Superseded[0], &p) != nil || p.Old != 1 || p.New != 2 {
			return nil, false
		}
	}
	if len(e.Duplicates) == 1 {
		var p struct {
			Keep    int   `json:"keep"`
			Members []int `json:"members"`
		}
		if modelcall.DecodeStrict(e.Duplicates[0], &p) != nil || p.Keep != 2 || len(p.Members) != 2 || !((p.Members[0] == 1 && p.Members[1] == 2) || (p.Members[0] == 2 && p.Members[1] == 1)) {
			return nil, false
		}
	}
	return ParseOutput(text, 2)
}

// rejudge judges one superseded statement and its successor again. An invalid
// output changes neither statement.
func (s Service) rejudge(ctx context.Context, job worker.Job) error {
	refs, err := RejudgeRefs(job, s.Version)
	if err != nil {
		return err
	}
	session, release, err := s.Store.Session(ctx)
	if err != nil {
		return err
	}
	defer release()
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	record := json.RawMessage("null")
	if !saved {
		if err := session.Ready(); err != nil {
			return err
		}
		var found bool
		record, found, err = session.RejudgePair(ctx, job, refs)
		if err != nil || !found {
			return err
		}
	}
	record, output, err := s.Generator.GenerateForJob(ctx, job, "compare", prompts.Must("compare"), "", record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the pair that its call used.
	var pair struct {
		Memories []json.RawMessage `json:"memories"`
		Refs     []json.RawMessage `json:"refs"`
		Rule     int               `json:"rule"`
	}
	if err := json.Unmarshal(record, &pair); err != nil {
		return err
	}
	if pair.Rule != s.Version || len(pair.Memories) != 2 || len(pair.Refs) != 2 {
		return memory.ErrInvalid
	}
	edges, valid := ParseRejudgeOutput(output)
	for _, edge := range edges {
		if edge.Old != 1 || edge.New != 2 {
			valid = false
		}
	}
	return session.ApplyRejudge(ctx, job, refs, edges, valid)
}
