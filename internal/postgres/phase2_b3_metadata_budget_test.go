package postgres

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type b3MetadataBudgetGold struct {
	ContractCommit       string
	LegacyBriefByteLimit int
	S6                   struct {
		BaselineCommit                                   string
		Entries                                          []string
		Query, TaskTitle, NotesUnit, NotesPlaceholder    string
		NotesByteLength                                  int
		BaselineBriefTemplate, BaselineBriefSHA256       string
		BaselineBriefBytes, ExpectedStructuredBriefBytes int
		PlaceName, PersonName, MetadataSuffix            string
		MemoryIDs                                        []memory.ID
		SourceID                                         memory.ID
	}
	S8 struct {
		Candidates, Selected                                               int
		Query, MemoryTemplate, PlaceName, PlaceAlias, PersonName           string
		EventMonth                                                         int
		EventPrecision, MetadataSuffixTemplate                             string
		ExpectedIndexes, ExcludedIndexes                                   []int
		ClaimDependencies, SourceDependencies, MinimumDeliveredMemoryBytes int
	}
}

func b3MetadataBudgetOracle(t *testing.T) b3MetadataBudgetGold {
	t.Helper()
	var g b3MetadataBudgetGold
	if err := json.Unmarshal(b3Gold(t)["metadataBudgetRuling"], &g); err != nil {
		t.Fatal(err)
	}
	if g.ContractCommit == "" || g.S6.BaselineBriefTemplate == "" || len(g.S6.MemoryIDs) == 0 || g.S8.Selected == 0 {
		t.Fatal("missing frozen R12a boundary oracle")
	}
	return g
}

func TestPhase2B3_S6_MetadataDoesNotConsumeDeputyOrManualByteBudget(t *testing.T) {
	gold := b3MetadataBudgetOracle(t)
	g := gold.S6
	notes := strings.Repeat(g.NotesUnit, g.NotesByteLength)
	want := strings.ReplaceAll(g.BaselineBriefTemplate, g.NotesPlaceholder, notes)
	if len(want) != g.BaselineBriefBytes || fmt.Sprintf("%x", sha256.Sum256([]byte(want))) != g.BaselineBriefSHA256 {
		t.Fatal("frozen lossless baseline template no longer reproduces the observed bytes")
	}
	if g.ExpectedStructuredBriefBytes != g.BaselineBriefBytes+len(g.MetadataSuffix)*len(g.MemoryIDs) {
		t.Fatal("frozen metadata budget oracle is inconsistent")
	}
	want = compareBaselineExpectation(t, want, true)
	memoryIDs := append(append([]memory.ID{}, g.MemoryIDs...), b3FixedID(22))
	for _, agent := range g.Entries {
		for _, structured := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_structured_%v", agent, structured), func(t *testing.T) {
				// Every group owns a fresh schema, so prior runs cannot alter the input.
				s := b1Store(t)
				scope := memory.Scope{OwnerID: b3FixedID(999), PrincipalID: "owner", IsOwner: true}
				f := b1Model(t, s, "边界交接已完成")
				b3BaselineData(t, s, scope)
				if structured {
					b3BaselineStructured(t, s, scope)
					b3Exec(t, s, `UPDATE entity_versions SET name=$3 WHERE owner_id=$1 AND entity_id=$2`, scope.OwnerID, b3FixedID(1), g.PersonName)
					b3Exec(t, s, `UPDATE entity_versions SET name=$3 WHERE owner_id=$1 AND entity_id=$2`, scope.OwnerID, b3FixedID(2), g.PlaceName)
				}
				task := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: g.TaskTitle}).Tasks[0].ID
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: task, Patch: asJSON(map[string]string{"notes": notes})})
				run := b1R2aRun(t, s, scope, f, task, agent, g.Query, "边界交接已完成")
				content := run.Brief
				if agent == "model" {
					// Check the complete Brief inside the actual captured HTTP request.
					request := f.last(t).Prompt
					start := strings.Index(request, run.Brief)
					if start < 0 {
						t.Fatal("actual deputy request does not contain the complete handoff")
					}
					content = request[start : start+len(run.Brief)]
				} else if len(f.all()) != 0 {
					t.Fatal("manual handoff unexpectedly called a model")
				}
				wantBytes := len(want)
				if structured {
					wantBytes += len(g.MetadataSuffix) * len(memoryIDs)
				}
				if len(content) != wantBytes {
					t.Errorf("handoff bytes=%d want %d", len(content), wantBytes)
				}
				if structured && len(content) <= gold.LegacyBriefByteLimit {
					t.Errorf("metadata boundary fixture must exceed the old %d-byte limit", gold.LegacyBriefByteLimit)
				}
				// Strip only the exact R12 suffix of selected claim lines. Original lines,
				// other context, IDs, whitespace and order are compared without changes.
				lines := strings.Split(content, "\n")
				seen := 0
				for i, line := range lines {
					for _, id := range memoryIDs {
						if strings.HasPrefix(line, "["+string(id)+"@1 / ") {
							seen++
							if structured {
								if !strings.HasSuffix(line, g.MetadataSuffix) {
									t.Errorf("selected memory %s lacks its full R12 suffix", id)
								} else {
									lines[i] = strings.TrimSuffix(line, g.MetadataSuffix)
								}
							}
						}
					}
				}
				if seen != len(memoryIDs) {
					t.Errorf("selected memory lines=%d want %d", seen, len(memoryIDs))
				}
				plain := strings.Join(lines, "\n")
				if plain != want {
					t.Errorf("handoff differs from pre-batch3 %s after removing only R12 suffixes: got %d bytes, want %d", g.BaselineCommit, len(plain), len(want))
				}
				refs := []memory.Ref{}
				for _, id := range memoryIDs {
					refs = append(refs, memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind})
				}
				refs = append(refs, memory.Ref{ID: g.SourceID, Version: 1, Kind: memory.SourceKind})
				if len(run.ContextVersions) != len(refs) {
					t.Errorf("handoff references=%d want %d", len(run.ContextVersions), len(refs))
				}
				for _, ref := range refs {
					b1HasRef(t, run.ContextVersions, ref, true)
					var count int
					if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM run_dependencies WHERE owner_id=$1 AND run_id=$2 AND memory_id=$3 AND memory_version=$4`, scope.OwnerID, run.ID, ref.ID, ref.Version).Scan(&count); err != nil || count != 1 {
						t.Errorf("delivered dependency %s count=%d err=%v", ref.ID, count, err)
					}
				}
			})
		}
	}
}

func TestPhase2B3_S8_LargeMetadataStillKeepsTheSameTopTwenty(t *testing.T) {
	gold := b3MetadataBudgetOracle(t).S8
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", gold.PlaceName, gold.PlaceAlias)
	person := b3Entity(t, s, scope, "person", gold.PersonName)
	base := b3Year(now, -1, time.December, 1)
	eventFrom := time.Date(now.Year()-1, time.Month(gold.EventMonth), 1, 0, 0, 0, 0, now.Location())
	eventTo := eventFrom.AddDate(0, 1, 0)
	refs := make([]memory.Ref, gold.Candidates)
	for i := range refs {
		at := base.Add(-time.Duration(i) * time.Hour)
		refs[i] = b3Claim(t, s, scope, b3ClaimSpec{
			Text: fmt.Sprintf(gold.MemoryTemplate, i), Subject: self, Said: &at,
			EventFrom: &eventFrom, EventTo: &eventTo, Precision: gold.EventPrecision,
			Mentions: []b3Mention{{place, "place"}, {person, "person"}},
		})
	}
	used := []memory.Ref{}
	for _, i := range gold.ExpectedIndexes {
		used = append(used, refs[i])
	}
	f.set(b3Used(used...))
	req := turnRequest(gold.Query)
	out := mustTurn(t, s, scope, req)
	if out.Turn.Reply == "" {
		t.Fatal("large metadata interrupted the normal secretary reply")
	}
	prompt := f.last(t).Prompt
	rows := []string{}
	memoryBytes := 0
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "[M") {
			rows = append(rows, line)
			memoryBytes += len(line) + 1
		}
	}
	if len(rows) != gold.Selected {
		t.Fatalf("selected memory rows=%d want %d", len(rows), gold.Selected)
	}
	if memoryBytes < gold.MinimumDeliveredMemoryBytes {
		t.Errorf("large-metadata fixture delivered %d memory bytes, want at least %d", memoryBytes, gold.MinimumDeliveredMemoryBytes)
	}
	for position, index := range gold.ExpectedIndexes {
		text := fmt.Sprintf(gold.MemoryTemplate, index)
		at := base.Add(-time.Duration(index) * time.Hour)
		suffix := strings.NewReplacer("{saidDate}", at.Format("2006-01-02"), "{year}", fmt.Sprint(now.Year()-1), "{placeName}", gold.PlaceName, "{personName}", gold.PersonName).Replace(gold.MetadataSuffixTemplate)
		if !strings.Contains(rows[position], text) {
			t.Errorf("row %d does not contain ranked memory %d", position, index)
		}
		if !strings.HasSuffix(rows[position], suffix) {
			t.Errorf("row %d lacks the complete frozen R12 suffix; actual place position=%d, person position=%d", position, strings.Index(rows[position], gold.PlaceName), strings.Index(rows[position], gold.PersonName))
			if position == 0 {
				t.Logf("actual first model memory line: %q", rows[position])
			}
		}
	}
	actual := b1Refs(t, s, scope, req.RequestID)
	for _, index := range gold.ExpectedIndexes {
		b1HasRef(t, actual, refs[index], true)
	}
	for _, index := range gold.ExcludedIndexes {
		b1Absent(t, prompt, fmt.Sprintf(gold.MemoryTemplate, index))
		b1HasRef(t, actual, refs[index], false)
	}
	claims, sources := 0, 0
	for _, ref := range actual {
		if ref.Kind == memory.ClaimKind {
			claims++
		}
		if ref.Kind == memory.SourceKind {
			sources++
		}
	}
	if claims != gold.ClaimDependencies || sources != gold.SourceDependencies {
		t.Errorf("delivered dependency counts memories=%d sources=%d, want %d/%d", claims, sources, gold.ClaimDependencies, gold.SourceDependencies)
	}
}
