package deputy

import (
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestOnlyAFinishedCurrentRunIsAdoptedOnce(t *testing.T) {
	done := workspace.Run{Status: "done", Kind: "breakdown"}
	if err := CheckAdoption(done, "subtasks", "- [ ] 第一步"); err != nil {
		t.Fatal(err)
	}
	adopted := done
	adopted.Adopted = &workspace.Adoption{As: "doc"}
	stale := done
	stale.StaleContext = true
	for name, run := range map[string]workspace.Run{"a run that failed": {Status: "failed"}, "a run that was adopted": adopted, "a run whose context changed": stale} {
		if err := CheckAdoption(run, "doc", "虚构的结果"); err != memory.ErrConflict {
			t.Fatal(name, err)
		}
	}
	if CheckAdoption(done, "doc", "  ") != memory.ErrInvalid || CheckAdoption(done, "email", "虚构") != memory.ErrInvalid {
		t.Fatal("an empty text or an unknown way is not valid")
	}
	if CheckAdoption(workspace.Run{Status: "done", Kind: "revise"}, "subtasks", "- [ ] 一步") != memory.ErrInvalid {
		t.Fatal("a revision is adopted as a document")
	}
}

func TestAnAdoptionMakesOneToOneHundredSubtasks(t *testing.T) {
	if got, err := Subtasks("前言\n- [ ] 第一步\n- [ ] 第二步"); err != nil || len(got) != 2 || got[1] != "第二步" {
		t.Fatal(got, err)
	}
	if _, err := Subtasks("- [ ] " + strings.Repeat("云", 700)); err != memory.ErrInvalid {
		t.Fatal("a subtask that is too long", err)
	}
	many := strings.Repeat("- [ ] 一步\n", 100)
	if got, err := Subtasks(many); err != nil || len(got) != 100 {
		t.Fatal(len(got), err)
	}
	if _, err := Subtasks(many + "- [ ] 第一百零一步"); err != memory.ErrInvalid {
		t.Fatal(err)
	}
	if _, err := Subtasks(" \n-\n"); err != memory.ErrInvalid {
		t.Fatal("a text without a step gives no subtask", err)
	}
}
