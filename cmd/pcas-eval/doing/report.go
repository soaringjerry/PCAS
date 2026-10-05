package doing

import (
	"fmt"
	"sort"
	"strings"
)

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func Aggregate(rows []Row, repeats int) ([]Summary, []Range) {
	type key struct {
		method, category string
		run              int
	}
	groups := map[key][]Row{}
	for _, r := range rows {
		for _, c := range []string{"all", r.Category} {
			k := key{r.Method, c, r.Run}
			groups[k] = append(groups[k], r)
		}
	}
	keys := []key{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.method != b.method {
			return a.method < b.method
		}
		if a.category != b.category {
			return a.category < b.category
		}
		return a.run < b.run
	})
	summaries := []Summary{}
	ranges := []Range{}
	for _, k := range keys {
		g := groups[k]
		s := Summary{Method: k.method, Category: k.category, Run: k.run, Tasks: len(g)}
		mt, mp, bt, bp, usable := 0, 0, 0, 0, 0
		for _, r := range g {
			mt += r.MustTotal
			mp += r.MustBoth
			bt += r.BonusTotal
			bp += r.BonusBoth
			s.ForbiddenHits += r.ForbiddenEither
			if r.Usable {
				usable++
			}
			s.InputCharsMean += float64(r.InputChars)
			s.ContextCharsMean += float64(r.ContextChars)
			s.ModelCalls += r.ModelCalls
			s.DoingMSMean += r.DoingMS
			s.TotalMSMean += r.TotalMS
			if len(r.Disagreements) > 0 {
				s.DisputedTasks++
			}
		}
		s.MustRate = ratio(mp, mt)
		s.BonusRate = ratio(bp, bt)
		s.UsableRate = ratio(usable, len(g))
		s.InputCharsMean /= float64(len(g))
		s.ContextCharsMean /= float64(len(g))
		s.DoingMSMean /= float64(len(g))
		s.TotalMSMean /= float64(len(g))
		summaries = append(summaries, s)
	}
	if repeats >= 3 {
		for _, s := range summaries {
			if s.Run != 1 {
				continue
			}
			r := Range{Method: s.Method, Category: s.Category, MustMin: s.MustRate, MustMax: s.MustRate, UsableMin: s.UsableRate, UsableMax: s.UsableRate, ForbiddenMin: s.ForbiddenHits, ForbiddenMax: s.ForbiddenHits}
			for _, v := range summaries {
				if v.Method != r.Method || v.Category != r.Category {
					continue
				}
				r.MustMin = min(r.MustMin, v.MustRate)
				r.MustMax = max(r.MustMax, v.MustRate)
				r.UsableMin = min(r.UsableMin, v.UsableRate)
				r.UsableMax = max(r.UsableMax, v.UsableRate)
				r.ForbiddenMin = min(r.ForbiddenMin, v.ForbiddenHits)
				r.ForbiddenMax = max(r.ForbiddenMax, v.ForbiddenHits)
			}
			r.IndifferencePP = 100 * max(r.MustMax-r.MustMin, r.UsableMax-r.UsableMin)
			ranges = append(ranges, r)
		}
	}
	return summaries, ranges
}
func WriteMarkdown(path string, r Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Phase 2.5 doing evaluation\n\nmodel `%s`; channel `%s`; fake=%t; repeats=%d; suite SHA `%s`; revision `%s`.\n\n", r.Model, r.Channel, r.Fake, r.Repeats, r.SuiteSHA, r.Revision)
	if p := r.Preparation; p != nil {
		fmt.Fprintf(&b, "Preparation complete=%t; wall ms=%.0f; claims=%d; retired=%d; cards=%d; handover=%t. These costs are outside the rows.\n\n", p.Complete, p.WallMS, p.Claims, p.Retired, p.Cards, p.Handover)
		fmt.Fprintln(&b, "| Preparation stage | Wall ms | Model calls | Failed calls | Input chars | Jobs done |\n|---|---:|---:|---:|---:|---:|")
		for _, s := range p.Stages {
			fmt.Fprintf(&b, "| %s | %.0f | %d | %d | %d | %d |\n", s.Stage, s.WallMS, s.ModelCalls, s.FailedCalls, s.InputChars, s.JobsDone)
		}
		fmt.Fprintln(&b)
	}
	if len(r.ResumeSources) > 0 {
		fmt.Fprintln(&b, "Transport repair: prior failed/canceled cells and attempted calls are outside the successful rows below; preparation is reused, each real invocation has its own preflight.")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Prior report SHA | Revision | Reused rows | Failed/canceled cells | Attempted calls in those cells |\n|---|---|---:|---:|---:|")
		for _, s := range r.ResumeSources {
			calls := 0
			for _, f := range s.Failures {
				calls += f.ModelCalls
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d |\n", s.SHA256, s.Revision, s.ReusedRows, len(s.Failures), calls)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "| Method | Category | Run | Tasks | Must % | Bonus % | Forbidden hits | Usable % | Input chars | Model calls | Doing ms | Total ms | Disputed |\n|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, s := range r.Summaries {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %.2f | %.2f | %d | %.2f | %.0f | %d | %.0f | %.0f | %d |\n", s.Method, s.Category, s.Run, s.Tasks, 100*s.MustRate, 100*s.BonusRate, s.ForbiddenHits, 100*s.UsableRate, s.InputCharsMean, s.ModelCalls, s.DoingMSMean, s.TotalMSMean, s.DisputedTasks)
	}
	fmt.Fprintln(&b, "\n| Method | Category | Must range % | Usable range % | Forbidden range | Indifference floor pp |\n|---|---|---:|---:|---:|---:|")
	for _, v := range r.Ranges {
		fmt.Fprintf(&b, "| %s | %s | %.2f–%.2f | %.2f–%.2f | %d–%d | %.2f |\n", v.Method, v.Category, 100*v.MustMin, 100*v.MustMax, 100*v.UsableMin, 100*v.UsableMax, v.ForbiddenMin, v.ForbiddenMax, v.IndifferencePP)
	}
	fmt.Fprintln(&b, "\nJudge disagreements (task IDs and check IDs only):")
	for _, row := range r.Rows {
		if len(row.Disagreements) > 0 {
			fmt.Fprintf(&b, "\n- run=%d %s %s: %s\n", row.Run, row.Method, row.Task, strings.Join(row.Disagreements, ","))
		}
	}
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "\n- %s\n", note)
	}
	return WriteAtomic(path, []byte(b.String()))
}
