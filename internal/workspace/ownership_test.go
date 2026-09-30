package workspace

import (
	"strings"
	"testing"
)

func authoredText(blocks []TextBlock) string {
	out := ""
	for _, b := range blocks {
		if len(b.Runs) == 0 {
			out += b.Text
		}
	}
	return out
}

func TestEditedMixedOwnership(t *testing.T) {
	blocks := []TextBlock{{Text: "My independent notes\n", Runs: []string{}}, {Text: "私密生成计划。", Runs: []string{"run-one"}}, {Text: "\nMy conclusion", Runs: []string{}}}
	for _, text := range []string{"My independent notes\n私密生成计划！\nMy conclusion", "My independent notes revised\n私密生成计划！\nMy conclusion", "My independent notes revised\n私密生成计划！\nMy conclusion\nNew independent paragraph"} {
		blocks = EditBlocks(blocks, text)
		if BlockText(blocks) != text {
			t.Fatalf("edit did not reconstruct text: %+v", blocks)
		}
		manual := authoredText(blocks)
		if !strings.Contains(manual, "My independent notes") || !strings.Contains(manual, "My conclusion") || strings.Contains(manual, "私密生成计划") {
			t.Fatalf("ownership lost: %+v", blocks)
		}
	}
	if !strings.Contains(authoredText(blocks), "New independent paragraph") {
		t.Fatalf("independent appended writing was protected: %+v", blocks)
	}
}

func TestGeneratedReplacementsStayDerived(t *testing.T) {
	blocks := []TextBlock{{Text: "private generated plan", Runs: []string{"a", "b"}}}
	blocks = EditBlocks(blocks, "a substantially revised plan")
	if BlockText(blocks) != "a substantially revised plan" || strings.Contains(authoredText(blocks), "substantially") {
		t.Fatalf("rewritten generated content lost dependencies: %+v", blocks)
	}
	for _, b := range blocks {
		if b.Text != "" && len(b.Runs) != 2 {
			t.Fatalf("multi-source dependencies lost: %+v", blocks)
		}
	}
}

func TestLargeEditIsBoundedAndConservative(t *testing.T) {
	text := strings.Repeat("secret ", 1000)
	blocks := []TextBlock{{Text: "manual\n", Runs: []string{}}, {Text: text, Runs: []string{"run"}}, {Text: "\nindependent", Runs: []string{}}}
	next := "manual\n" + strings.Replace(text, "secret", "revised", 1) + "\nindependent"
	blocks = EditBlocks(blocks, next)
	if BlockText(blocks) != next || strings.Contains(authoredText(blocks), "revised") || !strings.Contains(authoredText(blocks), "independent") {
		t.Fatal("large edit lost ownership")
	}
}

func TestEditedCopyBesideOriginalRetainsDependencies(t *testing.T) {
	blocks := []TextBlock{{Text: "Manual introduction\n", Runs: []string{}}, {Text: "Private launch plan for client Zephyr.", Runs: []string{"run"}}}
	blocks = EditBlocks(blocks, "Manual introduction\nPrivate launch plan for client Zephyr!\nPrivate launch plan for client Zephyr revised.")
	if strings.Contains(authoredText(blocks), "Zephyr") || !strings.Contains(authoredText(blocks), "Manual introduction") {
		t.Fatalf("edited copy lost provenance: %+v", blocks)
	}
}
