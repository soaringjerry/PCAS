package workspace

import "strings"

type TextBlock struct {
	Text string   `json:"text"`
	Runs []string `json:"runs"`
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
			labels = append(labels, b.Runs)
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
		if len(out) > 0 && strings.Join(out[len(out)-1].Runs, ",") == strings.Join(runs, ",") {
			out[len(out)-1].Text += string(chars)
			return
		}
		out = append(out, TextBlock{Text: string(chars), Runs: append([]string{}, runs...)})
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
		for _, line := range strings.Split(string(next[x:y]), "\n") {
			for _, block := range blocks {
				if len(block.Runs) > 0 && copiedBlock(line, block.Text) {
					for _, id := range block.Runs {
						if !hasRun(id, runs) {
							runs = append(runs, id)
						}
					}
				}
			}
		}
		appendText(next[x:y], runs)
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
	a, b := strings.Fields(added), strings.Fields(derived)
	if len(a) < 2 || len(b) < 2 {
		a, b = []string{}, []string{}
		for _, r := range []rune(strings.TrimSpace(added)) {
			a = append(a, string(r))
		}
		for _, r := range []rune(strings.TrimSpace(derived)) {
			b = append(b, string(r))
		}
		if min(len(a), len(b)) < 8 {
			return false
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
