package secretary

import (
	"context"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// fakeSession is one turn in memory: a model with fixed text and a
// workspace that the actions change.
type fakeSession struct {
	*fakeModel
	*fakeTurn
	contextErr, inputErr, targetErr error
	calls                           []string
}

func session(text string) *fakeSession {
	return &fakeSession{fakeModel: &fakeModel{text: text}, fakeTurn: &fakeTurn{desk: newDesk()}}
}
func (f *fakeSession) Context(context.Context) (View, error) {
	return View{Agent: "虚构秘书", Aliases: map[string]workspace.Item{}}, f.contextErr
}
func (f *fakeSession) Input(context.Context) (*Asking, string, error) {
	if f.inputErr != nil {
		return nil, "verify", f.inputErr
	}
	return asking("light", true), "verify", nil
}
func (f *fakeSession) Asked(*Asking) {}
func (f *fakeSession) BeginWrite(context.Context) error {
	f.calls = append(f.calls, "write")
	return nil
}
func (f *fakeSession) VerifyTargets(context.Context, Output) error {
	return f.targetErr
}
func (f *fakeSession) CheckFailed(_ context.Context, reason string) error {
	f.calls = append(f.calls, "check failed "+reason)
	return nil
}
func (f *fakeSession) RecordApplication(_ context.Context, stage, application, reason string) error {
	f.calls = append(f.calls, strings.TrimSpace(stage+" "+application+" "+reason))
	return nil
}
func (f *fakeSession) KeepSentence(_ context.Context, unordered bool) error {
	f.calls = append(f.calls, "keep")
	return nil
}
func (f *fakeSession) SaveSentence(context.Context) error {
	f.calls = append(f.calls, "save")
	return nil
}

func conduct(f *fakeSession, in Conducting) Conducted {
	in.Limits = limits
	out, err := Conduct(context.Background(), f, f, in)
	if err != nil {
		panic(err)
	}
	return out
}

func TestATurnThatFinishesSavesTheSentenceAndAppliesTheAnswer(t *testing.T) {
	f := session(`{"reply":"已建","remember":true,"actions":[{"op":"create_task","title":"虚构的任务"}]}`)
	got := conduct(f, Conducting{HasText: true})
	if !got.Finished || got.Agent != "虚构秘书" || got.Reply != "已建" || strings.Join(f.calls, ",") != "write,answer applied,save" {
		t.Fatalf("%+v %v", got, f.calls)
	}
	if len(got.Receipts) != 2 || got.Receipts[0].Op != "create_task" || got.Receipts[1].Op != "remember" || len(f.desk.items) != 1 {
		t.Fatalf("%+v", got.Receipts)
	}
	// A check-only turn keeps no sentence, so it says nothing about remembering.
	f = session(`{"reply":"好","remember":true}`)
	if got := conduct(f, Conducting{HasText: true, CheckOnly: true}); len(got.Receipts) != 0 {
		t.Fatalf("%+v", got.Receipts)
	}
}

func TestATurnThatCannotFinishKeepsTheSentenceAndChangesNothingElse(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(*fakeSession)
		in      Conducting
		calls   string
		text    string
	}{
		"no context":          {func(f *fakeSession) { f.contextErr = memory.ErrUnavailable }, Conducting{HasText: true}, "write,keep", "已记下原话；没有可用的模型，稍后会自动整理"},
		"lost its place":      {func(f *fakeSession) {}, Conducting{HasText: true, Unordered: true}, "write,keep", "已记下原话；这轮操作未完成，为避免覆盖后续改动，请重新说明要做的事"},
		"the model timed out": {func(f *fakeSession) { f.fakeModel.failure = context.DeadlineExceeded }, Conducting{HasText: true}, "write,answer not_applicable timeout,keep", "已记下原话；模型响应超时，稍后会自动整理"},
		"a target changed":    {func(f *fakeSession) { f.targetErr = memory.ErrConflict }, Conducting{HasText: true}, "write,answer not_applicable conflict,keep", "本轮要改动的事项已变更，请重试"},
		"attachments only":    {func(f *fakeSession) { f.fakeModel.failure = context.DeadlineExceeded }, Conducting{}, "write,answer not_applicable timeout,keep", "附件已存好；模型响应超时，稍后会自动整理"},
	} {
		f := session(`{"reply":"好","actions":[{"op":"create_task","title":"不该建的任务"}]}`)
		tc.prepare(f)
		got := conduct(f, tc.in)
		if got.Finished || strings.Join(f.calls, ",") != tc.calls || len(got.Receipts) != 1 || got.Receipts[0].Op != "capture" || got.Receipts[0].Text != tc.text || len(f.desk.items) != 0 || got.Reply != "" {
			t.Fatalf("%s: %+v %v", name, got, f.calls)
		}
	}
}

func TestAFailedSelfcheckKeepsTheDraftAndIsRecorded(t *testing.T) {
	f := session(`{"reply":"好","missingKeyInfo":true}`)
	f.fakeModel.checkErr = context.DeadlineExceeded
	got := conduct(f, Conducting{HasText: true})
	if !got.Finished || got.Reply != "好" || strings.Join(f.calls, ",") != "write,check failed timeout,selfcheck skipped timeout,answer applied,save" {
		t.Fatalf("%+v %v", got, f.calls)
	}
	if got.Receipts[0].Op != "selfcheck" || got.Receipts[0].Status != "skipped" {
		t.Fatalf("%+v", got.Receipts)
	}
}
