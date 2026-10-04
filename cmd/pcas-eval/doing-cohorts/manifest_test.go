package main

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func TestNumericReconstructionEscapesAndLineage(t *testing.T) {
	_, s, r, snap := fixture(t)
	text := "[虚构]\n<&\""
	snap.Entries[0].Text = text
	for i, row := range r.Rows {
		if row.Task == snap.Entries[0].Task && row.Method == "frozen-current" {
			r.Rows[i].ContextChars = utf8.RuneCountInString(text)
			r.Rows[i].InputChars = utf8.RuneCountInString(doing.AnswerSystem + doing.AnswerPrompt(s, s.Tasks[0], text))
		}
	}
	m := captureManifest{Schema: 1, SourceSHA: doing.SHA("source"), SnapshotSHA: doing.SHA("snapshot"), SuiteSHA: r.SuiteSHA, BaseSHA: doing.SHA("base"), AsOf: s.AsOf, ModelStart: r.StartedAt, SnapshotSuiteSHA: r.SuiteSHA, CaptureStart: "2026-10-04T23:00:00Z", CaptureEnd: "2026-10-04T23:01:00Z", Exposure: noiseExposure(s, snap)}
	for _, e := range snap.Entries {
		b, _ := json.Marshal(e.Text)
		m.Entries = append(m.Entries, numericEntry{Task: e.Task, RequestSHA: e.RequestSHA, ContextSHA: doing.SHA(e.Text), Chars: utf8.RuneCountInString(e.Text), JSONChars: utf8.RuneCount(b), Calls: 1})
	}
	metadata, sizes, err := recordedSnapshot(m, r, s, m.SourceSHA, m.BaseSHA)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateMeasured(r, s, r.SuiteSHA, metadata, sizes); err != nil {
		t.Fatal(err)
	}
	if metadata.Entries[0].Text != "" {
		t.Fatal("numeric reconstruction retained text")
	}
	sizes[s.Tasks[0].ID]++
	if validateMeasured(r, s, r.SuiteSHA, metadata, sizes) == nil {
		t.Fatal("incorrect escaped size accepted")
	}
	if _, _, err = recordedSnapshot(m, r, s, doing.SHA("different source"), m.BaseSHA); err == nil {
		t.Fatal("wrong source accepted")
	}
	m.CaptureEnd = "2026-10-05T00:00:01Z"
	if _, _, err = recordedSnapshot(m, r, s, m.SourceSHA, m.BaseSHA); err == nil {
		t.Fatal("cross-date capture accepted")
	}
}
