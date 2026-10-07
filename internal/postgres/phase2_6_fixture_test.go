package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// T1 is deterministic, fictitious, and independent of model output. Indices are
// stable oracle keys; database IDs are unique per fixture. No external DSN is used.
type phase26MemorySeed struct {
	Text, Language, Category string
	SaidAt                   time.Time
	Groups                   []int
}
type phase26EntitySeed struct{ Name, Kind, Language string }
type phase26GroupSeed struct {
	Name, Kind string
	Entity     int
	Members    []int
}
type phase26DeadlineGold struct {
	Memory                int
	Kind, State, Original string
	At                    *time.Time
	Recurrence            string
}
type phase26RuleGold struct {
	Memory    int
	Canonical int
	Scope     string
}
type phase26PairGold struct {
	Early, Recent, Group int
	Relation             string
}
type phase26Corpus struct {
	Now       time.Time
	Memories  []phase26MemorySeed
	Entities  []phase26EntitySeed
	Groups    []phase26GroupSeed
	Deadlines []phase26DeadlineGold
	Rules     []phase26RuleGold
	Pairs     []phase26PairGold
	Handover  map[string][]int
}

func phase26CorpusSeed() phase26Corpus {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	c := phase26Corpus{Now: now, Handover: map[string][]int{}}
	languages := []string{"zh", "en"}
	for i := 0; i < 1000; i++ {
		language := languages[i%2]
		name := fmt.Sprintf("虚构实体%04d", i)
		if i%2 == 1 {
			name = fmt.Sprintf("FictitiousEntity%04d", i)
		}
		// Four category groups have Chinese names. Balance the real directory
		// as well as entities by moving two English names into grouped entities
		// and two Chinese names into nongrouping entities.
		if i == 8 || i == 10 {
			name, language = fmt.Sprintf("FictitiousEntity%04d", i), "en"
		}
		if i == 297 || i == 299 {
			name, language = fmt.Sprintf("虚构实体%04d", i), "zh"
		}
		kind := []string{"thing", "place", "organization"}[i%3]
		if i < 296 {
			kind = []string{"person", "project", "topic", "area"}[i%4]
		}
		if i == 998 {
			kind = "self"
			name = "虚构本人"
		}
		if i == 1 {
			name = "Al"
		}
		if i == 3 {
			name = "Bo"
		}
		if i == 5 {
			name = "Ari"
		}
		if i == 7 {
			name = "Cal"
		}
		c.Entities = append(c.Entities, phase26EntitySeed{name, kind, language})
	}
	categories := []string{"rule", "identity", "taste", "goal", "progress", "event", "opinion", "other_person", "unknown"}
	for i := 0; i < 5000; i++ {
		category := categories[4+i%5]
		switch {
		case i < 366:
			category = "rule"
		case i < 556:
			category = "identity"
		case i < 929:
			category = "taste"
		case i < 1377:
			category = "goal"
		}
		text := fmt.Sprintf("虚构便签%04d：测试样本中保存了一枚编号为%04d的陶瓷标本。", i, i)
		if i%2 == 1 {
			text = fmt.Sprintf("Fictitious note %04d: the sample archive holds ceramic specimen %04d.", i, i)
		}
		said := now.AddDate(0, -9, 0).Add(time.Duration(i) * time.Minute)
		c.Memories = append(c.Memories, phase26MemorySeed{Text: text, Language: languages[i%2], Category: category, SaidAt: said})
	}
	// The first ten entity groups reproduce the long tail. Remaining groups
	// intentionally include 1/2-member groups. Last 80 memories are ungrouped.
	large := []int{1200, 650, 600, 550, 500, 450, 420, 390, 360, 330}
	for g := 0; g < 296; g++ {
		count := 8 + g%19
		if g < 10 {
			count = large[g]
		}
		if g == 294 {
			count = 1
		}
		if g == 295 {
			count = 2
		}
		group := phase26GroupSeed{Name: c.Entities[g].Name, Kind: c.Entities[g].Kind, Entity: g}
		for j := 0; j < count; j++ {
			index := (g*37 + j) % 4920
			group.Members = append(group.Members, index)
			c.Memories[index].Groups = append(c.Memories[index].Groups, g)
		}
		c.Groups = append(c.Groups, group)
	}
	// Cover the gaps between the small groups without enlarging the ten fixed
	// long-tail groups or the two tiny groups. Only the final 80 stay ungrouped.
	for i := 0; i < 4920; i++ {
		if len(c.Memories[i].Groups) == 0 {
			g := 10 + i%284
			c.Groups[g].Members = append(c.Groups[g].Members, i)
			c.Memories[i].Groups = append(c.Memories[i].Groups, g)
		}
	}
	for _, cat := range categories[:4] {
		name := map[string]string{"rule": "对助手的要求", "identity": "身份", "taste": "口味", "goal": "目标"}[cat]
		group := phase26GroupSeed{Name: name, Kind: "self", Entity: -1}
		for i := range c.Memories {
			if c.Memories[i].Category == cat {
				group.Members = append(group.Members, i)
				c.Memories[i].Groups = append(c.Memories[i].Groups, len(c.Groups))
			}
		}
		c.Groups = append(c.Groups, group)
	}
	// 366 raw requirements, 364 distinct before applying the planted correction,
	// 363 effective after comparison; 26 raw unrestricted / 24 distinct.
	// Two old/recent duplicate pairs straddle the old 300-item window.
	for i := 0; i < 366; i++ {
		canonical := i
		if i >= 364 {
			canonical = i - 364
		}
		scope := ""
		if canonical >= 24 {
			scope = "Preparing a fictitious exhibition proposal"
		}
		text := fmt.Sprintf("虚构助手要求%03d：展示第%03d种陶瓷样本时附上样本编号。", canonical, canonical)
		if i%2 == 1 {
			text = fmt.Sprintf("Fictitious assistant requirement %03d: include specimen IDs for ceramic sample type %03d.", canonical, canonical)
		}
		if scope == "" {
			text = fmt.Sprintf("每次回复都附上虚构校验标记%03d。", canonical)
			if i%2 == 1 {
				text = fmt.Sprintf("Always append fictitious verification marker %03d to every reply.", canonical)
			}
		}
		if scope != "" {
			if i%2 == 0 {
				text = "筹备虚构展览提案时，" + text
			} else {
				text = "When preparing a fictitious exhibition proposal, " + text
			}
		}
		c.Memories[i].Text = text
		c.Rules = append(c.Rules, phase26RuleGold{i, canonical, scope})
	}
	c.Pairs = []phase26PairGold{{0, 364, 296, "duplicate"}, {1, 365, 296, "duplicate"}, {24, 363, 0, "superseded"}}
	// Both correction memories are scoped requirements in the same two groups.
	c.Memories[24].Text = "对助手的虚构要求：筹备虚构展览提案时，青湾展的预算按一百元计算。"
	c.Memories[363].Text = "Fictitious assistant requirement: when preparing an exhibition proposal, use a budget of 200 units for Azure Bay, replacing the old 100-unit budget."
	for _, i := range []int{364, 365, 363} {
		c.Memories[i].SaidAt = now.Add(-time.Hour)
	}
	// Explicit expected input subsets, supplemented by all rule/deadline indices.
	c.Handover["identity"] = []int{366, 367}
	c.Handover["taste"] = []int{556, 557}
	c.Handover["goal"] = []int{930, 931}
	for _, item := range []struct {
		index  int
		zh, en string
	}{
		{366, "虚构本人在青湾陶瓷档案馆担任馆员。", "The fictitious owner is a curator at the Azure Bay ceramics archive."},
		{556, "虚构本人一直偏爱蓝色陶瓷。", "The fictitious owner repeatedly prefers blue ceramics."},
		{930, "虚构本人近期目标是完成青湾陶瓷展。", "The fictitious owner's recent goal is to finish the Azure Bay ceramics exhibition."},
	} {
		c.Memories[item.index].Text = item.zh
		c.Memories[item.index+1].Text = item.en
		c.Memories[item.index].SaidAt = now.Add(-2 * time.Hour)
		c.Memories[item.index+1].SaidAt = now.Add(-time.Hour)
	}
	// 48 independent deadlines: 12 future, 12 expired, 12 recurring, 12 unclear.
	// Includes memories without groups and the two tiny groups; no expected result
	// is preloaded into the deadlines table. All concrete dates follow SaidAt.
	for i := 0; i < 48; i++ {
		index := 1800 + i
		if i >= 40 {
			index = 4920 + i
		}
		if i == 0 {
			index = c.Groups[294].Members[0]
		}
		if i == 1 {
			index = c.Groups[295].Members[0]
		}
		zh := index%2 == 0
		gold := phase26DeadlineGold{Memory: index}
		switch i / 12 {
		case 0:
			gold.Kind = "deadline"
			gold.State = "future"
			at := time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC)
			gold.At = &at
			gold.Original = "虚构展览提案下个月五号下午五点前交。"
			if !zh {
				gold.Original = "Submit the fictitious exhibition proposal by November 5, 2026 at 5 pm UTC."
			}
		case 1:
			gold.Kind = "appointment"
			gold.State = "expired"
			at := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
			gold.At = &at
			gold.Original = "虚构展览碰头会九月底下午两点开始，还不知是否完成。"
			if !zh {
				gold.Original = "The fictitious exhibition meeting is September 30, 2026 at 2 pm UTC; completion is unknown."
			}
		case 2:
			gold.Kind = "recurring"
			gold.State = "recurring"
			gold.Recurrence = "weekly Tuesday 14:00 UTC"
			gold.Original = "虚构陶瓷班逢礼拜二下午两点开课。"
			if !zh {
				gold.Original = "Fictitious ceramics class meets every Tuesday at 2 pm UTC."
			}
		case 3:
			gold.Kind = "deadline"
			gold.State = "unclear"
			gold.Original = "虚构展览提案等下一轮评审前交，具体日期还没定。"
			if !zh {
				gold.Original = "Submit the fictitious exhibition proposal before the next review round; the date is undecided."
			}
		}
		c.Memories[index].Text = fmt.Sprintf("%s [sample %02d]", gold.Original, i)
		if i/12 == 0 {
			c.Memories[index].SaidAt = now.Add(-time.Hour)
		}
		gold.Original = c.Memories[index].Text
		c.Deadlines = append(c.Deadlines, gold)
	}
	for _, r := range c.Rules {
		c.Handover["rule"] = append(c.Handover["rule"], r.Memory)
	}
	for _, d := range c.Deadlines {
		c.Handover["deadline"] = append(c.Handover["deadline"], d.Memory)
	}
	return c
}

func TestPhase26T1CorpusOracle(t *testing.T) {
	c := phase26CorpusSeed()
	if len(c.Memories) != 5000 || len(c.Groups) != 300 || len(c.Entities) != 1000 {
		t.Fatal("T1 scale mismatch")
	}
	if !reflect.DeepEqual(c, phase26CorpusSeed()) {
		t.Fatal("fixture is not deterministic")
	}
	for name, langs := range map[string][]string{"memory": {}, "entity": {}, "group": {}} {
		if name == "memory" {
			for _, m := range c.Memories {
				langs = append(langs, m.Language)
				if (m.Language == "zh") != strings.ContainsFunc(m.Text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
					t.Fatalf("memory language/content mismatch: %q", m.Text)
				}
			}
		}
		if name == "entity" {
			for _, e := range c.Entities {
				langs = append(langs, e.Language)
			}
		}
		if name == "group" {
			for _, g := range c.Groups {
				lang := "en"
				if strings.ContainsFunc(g.Name, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
					lang = "zh"
				}
				langs = append(langs, lang)
			}
		}
		zh, en := 0, 0
		for _, lang := range langs {
			if lang == "zh" {
				zh++
			} else if lang == "en" {
				en++
			} else {
				t.Fatal("unknown language")
			}
		}
		if zh != en {
			t.Fatalf("%s languages: zh=%d en=%d", name, zh, en)
		}
	}
	large, max := 0, 0
	shortNames := map[int]int{}
	for _, entity := range c.Entities {
		if (entity.Language == "zh") != strings.ContainsFunc(entity.Name, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			t.Fatalf("entity language/content mismatch: %q", entity.Name)
		}
		if entity.Language == "en" && (len(entity.Name) == 2 || len(entity.Name) == 3) {
			shortNames[len(entity.Name)]++
		}
	}
	if shortNames[2] < 2 || shortNames[3] < 2 {
		t.Fatalf("missing two/three letter English alias traps: %v", shortNames)
	}
	for g, group := range c.Groups {
		if len(group.Members) > 300 {
			large++
		}
		if len(group.Members) > max {
			max = len(group.Members)
		}
		seen := map[int]bool{}
		for _, i := range group.Members {
			if i < 0 || i >= 5000 || seen[i] {
				t.Fatalf("group %d invalid/duplicate member %d", g, i)
			}
			seen[i] = true
		}
	}
	if large < 10 || max <= 1000 {
		t.Fatalf("large groups=%d largest=%d", large, max)
	}
	ungrouped := 0
	for _, m := range c.Memories {
		if len(m.Groups) == 0 {
			ungrouped++
		}
	}
	if ungrouped != 80 {
		t.Fatalf("ungrouped=%d want 80", ungrouped)
	}
	states := map[string]int{}
	seenDeadlines := map[int]bool{}
	for _, d := range c.Deadlines {
		states[d.State]++
		if seenDeadlines[d.Memory] {
			t.Fatal("duplicate deadline source")
		}
		seenDeadlines[d.Memory] = true
		if d.Original != c.Memories[d.Memory].Text {
			t.Fatal("deadline provenance mismatch")
		}
		if d.At != nil && d.At.Before(c.Memories[d.Memory].SaidAt) {
			t.Fatal("deadline predates utterance")
		}
	}
	if !reflect.DeepEqual(states, map[string]int{"future": 12, "expired": 12, "recurring": 12, "unclear": 12}) {
		t.Fatalf("deadline states=%v", states)
	}
	unique, unrestricted := map[int]bool{}, map[int]bool{}
	for _, r := range c.Rules {
		unique[r.Canonical] = true
		if r.Scope == "" {
			unrestricted[r.Canonical] = true
		}
	}
	if len(c.Rules) != 366 || len(unique) != 364 || len(unrestricted) != 24 {
		t.Fatal("requirement oracle mismatch")
	}
	for _, p := range c.Pairs {
		positions := map[int]int{}
		for j, i := range c.Groups[p.Group].Members {
			positions[i] = j
		}
		a, okA := positions[p.Early]
		b, okB := positions[p.Recent]
		if !okA || !okB || b-a <= 300 || !c.Memories[p.Early].SaidAt.Before(c.Memories[p.Recent].SaidAt) {
			t.Fatalf("pair does not cross old window: %+v", p)
		}
	}
	t.Logf("T1 oracle: memories=5000 groups=300 entities=1000 large_groups=%d largest=%d ungrouped=%d deadlines=%v raw_rules=366 unique_rules=364 unrestricted=24", large, max, ungrouped, states)
}

// All Docker commands are pinned to the local socket, never an inherited remote
// context. Cleanup targets only the exact container created by this test.
func phase26DisposableStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	docker := func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", append([]string{"--host=unix:///var/run/docker.sock"}, args...)...)
		for _, e := range os.Environ() {
			key, _, _ := strings.Cut(e, "=")
			switch key {
			case "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH":
				continue
			}
			cmd.Env = append(cmd.Env, e)
		}
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	name := "pcas-phase2_6-t-" + string(memory.NewID())
	// Real PostgreSQL WAL may approach 1 GiB during repeated scale writes.
	// Allow 2 GiB of ephemeral storage; allocation follows actual use.
	out, err := docker(ctx, "run", "--detach", "--rm", "--name", name, "--label", "pcas.acceptance=phase2_6-T", "--tmpfs", "/var/lib/postgresql/data:rw,size=2g", "--env", "POSTGRES_PASSWORD=fictitious-test-password", "--publish", "127.0.0.1::5432", "pgvector/pgvector:0.8.2-pg16")
	if err != nil {
		t.Fatalf("create owned test container: %v: %s", err, out)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if t.Failed() {
			logs, _ := docker(cleanupCtx, "logs", "--tail", "35", name)
			t.Logf("owned disposable database diagnostics: %s", logs)
		}
		out, err := docker(cleanupCtx, "rm", "--force", "--volumes", name)
		if err != nil {
			t.Errorf("remove owned container %s: %v: %s", name, err, out)
		}
	})
	address, err := docker(ctx, "port", name, "5432/tcp")
	if err != nil {
		t.Fatalf("test container port: %v: %s", err, address)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("expected one loopback test port, got %q", address)
	}
	dsn := "postgres://postgres:fictitious-test-password@" + address + "/postgres?sslmode=disable"
	deadline := time.Now().Add(20 * time.Second)
	var s *Store
	for time.Now().Before(deadline) {
		s, err = Open(ctx, dsn)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err != nil {
		t.Fatalf("owned database readiness: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

type phase26LoadedFixture struct {
	Store            *Store
	Context          context.Context
	Scope            memory.Scope
	Corpus           phase26Corpus
	Claims, Entities []memory.ID
	Sources          []memory.Ref
}

func phase26LoadFixture(t *testing.T) *phase26LoadedFixture {
	t.Helper()
	s, ctx := phase26DisposableStore(t)
	f := &phase26LoadedFixture{Store: s, Context: ctx, Scope: memory.Scope{OwnerID: memory.NewID(), PrincipalID: "fictitious-phase26-owner", IsOwner: true}, Corpus: phase26CorpusSeed()}
	// 200 entity / 100 claim batches respect the existing commit API. No rows
	// are discarded. Limits apply only to constructing the test precondition.
	for i := 0; i < 1000; i += 200 {
		in := memory.CommitRequest{RequestID: memory.NewID()}
		for _, e := range f.Corpus.Entities[i : i+200] {
			id := memory.NewID()
			f.Entities = append(f.Entities, id)
			in.Entities = append(in.Entities, memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: id, Version: 1, Kind: memory.EntityKind}, State: "active"}, Type: e.Kind, Name: e.Name})
		}
		if _, err := s.Commit(ctx, f.Scope, in); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5000; i += 100 {
		var body strings.Builder
		for _, m := range f.Corpus.Memories[i : i+100] {
			body.WriteString(m.Text)
			body.WriteByte('\n')
		}
		source, err := s.Ingest(ctx, f.Scope, memory.IngestRequest{Connector: "phase2_6-fictitious", ExternalID: fmt.Sprintf("batch-%d", i/100), Title: "Fictitious acceptance source", Text: body.String()})
		if err != nil {
			t.Fatal(err)
		}
		f.Sources = append(f.Sources, source.Ref)
		in := memory.CommitRequest{RequestID: memory.NewID()}
		for _, m := range f.Corpus.Memories[i : i+100] {
			id := memory.NewID()
			f.Claims = append(f.Claims, id)
			value, _ := json.Marshal(m.Text)
			ref := memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind}
			in.Claims = append(in.Claims, memory.Claim{Revision: memory.Revision{Ref: ref, State: "active", ExpressedAt: &m.SaidAt}, SubjectID: f.Entities[998], Predicate: "fictitious_acceptance_note", Value: value, Nature: "fact", Acquisition: "direct", Confirmation: "unknown"})
			in.Evidence = append(in.Evidence, memory.Evidence{ID: memory.NewID(), Source: source.Ref, Target: ref, Acquisition: "direct", Stance: "supports"})
		}
		if _, err := s.Commit(ctx, f.Scope, in); err != nil {
			t.Fatal(err)
		}
	}
	// SQL only establishes labels/memberships, not output under acceptance.
	// Real migrations, constraints and triggers stay enabled.
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for i, m := range f.Corpus.Memories {
			batch.Queue(`UPDATE claim_revisions SET category=$3,durable=true WHERE owner_id=$1 AND claim_id=$2 AND version=1`, f.Scope.OwnerID, f.Claims[i], m.Category)
		}
		results := tx.SendBatch(ctx, batch)
		if err := results.Close(); err != nil {
			return err
		}
		rows := [][]any{}
		for g, group := range f.Corpus.Groups {
			if group.Entity < 0 {
				continue
			}
			for _, i := range group.Members {
				rows = append(rows, []any{f.Scope.OwnerID, f.Claims[i], 1, f.Entities[g], group.Kind})
			}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"claim_mentions"}, []string{"owner_id", "claim_id", "claim_version", "entity_id", "role"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Fresh disposable databases otherwise lack production-scale planner statistics.
	// Analyze only this owned synthetic database; retain all real indexes/triggers.
	if _, err := s.pool.Exec(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPhase26T1LoadedScaleAndProvenance(t *testing.T) {
	start := time.Now()
	f := phase26LoadFixture(t)
	s, ctx, owner := f.Store, f.Context, f.Scope.OwnerID
	counts := map[string]int{}
	for _, table := range []string{"claims", "entities", "evidence", "sources"} {
		var n int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE owner_id=$1", owner).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts[table] = n
	}
	if !reflect.DeepEqual(counts, map[string]int{"claims": 5000, "entities": 1000, "evidence": 5000, "sources": 50}) {
		t.Fatalf("loaded scale=%v", counts)
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id::text,r.state,cl.retired,c.category,c.value,rv.expressed_at FROM memory_records r JOIN claims cl ON (cl.owner_id,cl.id)=(r.owner_id,r.id) JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version) JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version) WHERE r.owner_id=$1`, owner)
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]int{}
	for i, id := range f.Claims {
		index[string(id)] = i
	}
	seen := map[string]bool{}
	for rows.Next() {
		var id, state, retired, category string
		var value []byte
		var said time.Time
		if err := rows.Scan(&id, &state, &retired, &category, &value, &said); err != nil {
			t.Fatal(err)
		}
		i, ok := index[id]
		if !ok || seen[id] {
			t.Fatal("unexpected/duplicate current memory")
		}
		seen[id] = true
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			t.Fatal(err)
		}
		want := f.Corpus.Memories[i]
		if state != "active" || retired != "" || category != want.Category || text != want.Text || !said.Equal(want.SaidAt) {
			t.Fatalf("loaded memory %d differs from oracle", i)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(seen) != 5000 {
		t.Fatal("missing current memories")
	}
	var broken int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM claims c WHERE c.owner_id=$1 AND NOT claim_source_is_current(c.owner_id,c.id,1,$2)`, owner, f.Corpus.Now).Scan(&broken); err != nil {
		t.Fatal(err)
	}
	if broken != 0 {
		t.Fatalf("memories without current provenance=%d", broken)
	}
	// The membership view is only the loading adapter. T2 must compare all new
	// readers against the independent graph, not use this view as its oracle.
	rows, err = s.pool.Query(ctx, `SELECT key,name,kind,claim_id::text FROM status_current_members WHERE owner_id=$1`, owner)
	if err != nil {
		t.Fatal(err)
	}
	groupKeys := map[string]int{}
	for g, group := range f.Corpus.Groups {
		key := "self:" + f.Corpus.Memories[group.Members[0]].Category
		if group.Entity >= 0 {
			key = "entity:" + string(f.Entities[group.Entity])
		}
		groupKeys[key] = g
	}
	actual := map[int]map[int]bool{}
	for rows.Next() {
		var key, name, kind, id string
		if err := rows.Scan(&key, &name, &kind, &id); err != nil {
			t.Fatal(err)
		}
		g, ok := groupKeys[key]
		if !ok {
			t.Fatalf("unexpected loaded group %s", key)
		}
		if name != f.Corpus.Groups[g].Name || kind != f.Corpus.Groups[g].Kind {
			t.Fatalf("group %d name/kind differs from planted catalog", g)
		}
		i, ok := index[id]
		if !ok {
			t.Fatal("unexpected member")
		}
		if actual[g] == nil {
			actual[g] = map[int]bool{}
		}
		if actual[g][i] {
			t.Fatal("duplicate membership")
		}
		actual[g][i] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(actual) != 300 {
		t.Fatalf("loaded groups=%d", len(actual))
	}
	for g, group := range f.Corpus.Groups {
		want := map[int]bool{}
		for _, i := range group.Members {
			want[i] = true
		}
		if !reflect.DeepEqual(actual[g], want) {
			t.Fatalf("group %d differs from independently planted membership", g)
		}
	}
	var results int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM deadlines WHERE owner_id=$1)+(SELECT count(*) FROM model_usage WHERE owner_id=$1)`, owner).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if results != 0 {
		t.Fatal("fixture precomputed acceptance output or invoked a model")
	}
	t.Logf("T1 load verified: %v groups=300 exact memberships and all content/provenance; setup=%s", counts, time.Since(start))
}
