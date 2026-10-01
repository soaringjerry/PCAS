package workspace

import (
	"strings"
	"unicode"
)

type TextBlock struct {
	Text string   `json:"text"`
	Runs []string `json:"runs"`
	// DeskActions are server-written action identities, separate from agent Runs.
	DeskActions []string `json:"deskActions,omitempty"`
}

func BlockText(blocks []TextBlock) string {
	var out strings.Builder
	for _, b := range blocks {
		out.WriteString(b.Text)
	}
	return out.String()
}
func hasRun(id string, runs []string) bool {
	for _, run := range runs {
		if id == run {
			return true
		}
	}
	return false
}

// Edits inherit provenance from the replaced range. Insertions inside a
// generated block inherit that block; independent paragraphs outside it are
// authored. Equal characters retain their ownership through punctuation edits.
// Large replacements use a conservative bounded fallback.
func EditBlocks(blocks []TextBlock, text string) []TextBlock {
	old := []rune(BlockText(blocks))
	next := []rune(text)
	if string(old) == text {
		return blocks
	}
	labels := make([][]string, 0, len(old))
	for _, b := range blocks {
		for range []rune(b.Text) {
			labels = append(labels, ownershipLabels(b))
		}
	}
	union := func(start, end int) []string {
		out := []string{}
		for _, runs := range labels[start:end] {
			for _, id := range runs {
				if !hasRun(id, out) {
					out = append(out, id)
				}
			}
		}
		return out
	}
	out := []TextBlock{}
	appendText := func(chars []rune, runs []string) {
		if len(chars) == 0 {
			return
		}
		if len(out) > 0 && strings.Join(ownershipLabels(out[len(out)-1]), ",") == strings.Join(runs, ",") {
			out[len(out)-1].Text += string(chars)
			return
		}
		block := TextBlock{Text: string(chars), Runs: []string{}}
		for _, id := range runs {
			if strings.HasPrefix(id, "desk:") {
				block.DeskActions = append(block.DeskActions, strings.TrimPrefix(id, "desk:"))
			} else {
				block.Runs = append(block.Runs, id)
			}
		}
		out = append(out, block)
	}
	replace := func(a, b, x, y int) {
		runs := union(a, b)
		if a == b && a > 0 && a < len(labels) {
			// A newline starts a separate authored paragraph at an ownership boundary.
			if strings.Join(labels[a-1], ",") == strings.Join(labels[a], ",") {
				runs = labels[a]
			}
		}
		if a == b && a == len(old) && a > 0 && len(next[x:y]) > 0 && next[x] != '\n' {
			runs = labels[a-1]
		}
		// A pasted/edited copy outside the original block still depends on its
		// inputs. Compare new edit hunks against current ownership blocks, not
		// the originally adopted string (which may already have been rewritten).
		// Attribute copied paragraphs separately. One copied line must not
		// relabel all the independent writing in the same insertion as derived.
		for _, line := range strings.SplitAfter(string(next[x:y]), "\n") {
			lineRuns := append([]string{}, runs...)
			for _, block := range blocks {
				if len(ownershipLabels(block)) > 0 && copiedBlock(line, block.Text) {
					for _, id := range ownershipLabels(block) {
						if !hasRun(id, lineRuns) {
							lineRuns = append(lineRuns, id)
						}
					}
				}
			}
			appendText([]rune(line), lineRuns)
		}
	}
	if int64(len(old)+1)*int64(len(next)+1) > 4_000_000 {
		p := 0
		for p < len(old) && p < len(next) && old[p] == next[p] {
			p++
		}
		s := 0
		for s < len(old)-p && s < len(next)-p && old[len(old)-1-s] == next[len(next)-1-s] {
			s++
		}
		for i := 0; i < p; i++ {
			appendText(old[i:i+1], labels[i])
		}
		replace(p, len(old)-s, p, len(next)-s)
		for i := len(old) - s; i < len(old); i++ {
			appendText(old[i:i+1], labels[i])
		}
		return out
	}
	width := len(next) + 1
	dp := make([]int32, (len(old)+1)*width)
	for i := len(old) - 1; i >= 0; i-- {
		for j := len(next) - 1; j >= 0; j-- {
			if old[i] == next[j] {
				dp[i*width+j] = dp[(i+1)*width+j+1] + 1
			} else {
				dp[i*width+j] = max(dp[(i+1)*width+j], dp[i*width+j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(old) || j < len(next) {
		if i < len(old) && j < len(next) && old[i] == next[j] {
			appendText(next[j:j+1], labels[i])
			i++
			j++
			continue
		}
		a, x := i, j
		for i < len(old) || j < len(next) {
			if i < len(old) && j < len(next) && old[i] == next[j] {
				break
			}
			if j < len(next) && (i == len(old) || dp[i*width+j+1] > dp[(i+1)*width+j]) {
				j++
			} else {
				i++
			}
		}
		replace(a, i, x, j)
	}
	return out
}

func copiedBlock(added, derived string) bool {
	// Punctuation does not change a copied name or value. In particular,
	// "client Zephyr" still copies "client Zephyr." and a standalone PIN
	// still copies that PIN from a longer generated sentence.
	words := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
	}
	a, b := words(added), words(derived)
	if len(a) < 2 || len(b) < 2 {
		left, right := strings.Join(a, ""), strings.Join(b, "")
		// Short fragments need an exact contiguous match; fuzzy comparison of
		// a few characters would sweep up unrelated independently authored text.
		// This is a bounded copy heuristic, not semantic authorship recovery.
		if min(len([]rune(left)), len([]rune(right))) < 8 {
			return len([]rune(left)) >= 4 && strings.Contains(right, left)
		}
		a, b = []string{}, []string{}
		for _, r := range []rune(left) {
			a = append(a, string(r))
		}
		for _, r := range []rune(right) {
			b = append(b, string(r))
		}
	}
	if len(a)*len(b) > 100000 {
		// Bound fuzzy copy checks without dropping provenance for long pasted
		// paragraphs. Compare their first 300 tokens/characters conservatively.
		a = a[:min(len(a), 300)]
		b = b[:min(len(b), 300)]
	}
	row := make([]int, len(b)+1)
	for _, value := range a {
		previous := 0
		for j, other := range b {
			before := row[j+1]
			if value == other {
				row[j+1] = previous + 1
			} else {
				row[j+1] = max(row[j], row[j+1])
			}
			previous = before
		}
	}
	return row[len(b)]*100 >= min(len(a), len(b))*80
}

// Prefixes exist only in the edit algorithm's labels, never in persisted Runs.
func ownershipLabels(block TextBlock) []string {
	out := append([]string{}, block.Runs...)
	for _, id := range block.DeskActions {
		out = append(out, "desk:"+id)
	}
	return out
}

// CopyOrigins carries provenance to copied paragraphs in another tracked
// field. Independent paragraphs retain their existing labels.
func CopyOrigins(blocks, sources []TextBlock) []TextBlock {
	out := []TextBlock{}
	for _, block := range blocks {
		for _, line := range strings.SplitAfter(block.Text, "\n") {
			if line == "" {
				continue
			}
			next := TextBlock{Text: line, Runs: append([]string{}, block.Runs...), DeskActions: append([]string{}, block.DeskActions...)}
			for _, source := range sources {
				if (len(source.Runs) == 0 && len(source.DeskActions) == 0) || !copiedBlock(line, source.Text) {
					continue
				}
				for _, id := range source.Runs {
					if !hasRun(id, next.Runs) {
						next.Runs = append(next.Runs, id)
					}
				}
				for _, id := range source.DeskActions {
					if !hasRun(id, next.DeskActions) {
						next.DeskActions = append(next.DeskActions, id)
					}
				}
			}
			if len(out) > 0 && strings.Join(ownershipLabels(out[len(out)-1]), ",") == strings.Join(ownershipLabels(next), ",") {
				out[len(out)-1].Text += next.Text
			} else {
				out = append(out, next)
			}
		}
	}
	return out
}
