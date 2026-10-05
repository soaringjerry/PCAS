package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func doingFreezeArgs(args []string) ([]string, bool) {
	for i, a := range args {
		if a == "-mode=doing-freeze" || a == "--mode=doing-freeze" {
			return append(append([]string{}, args[:i]...), args[i+1:]...), true
		}
		if (a == "-mode" || a == "--mode") && i+1 < len(args) && args[i+1] == "doing-freeze" {
			return append(append([]string{}, args[:i]...), args[i+2:]...), true
		}
	}
	return nil, false
}

// No real model or real-data export: capture the existing product route on the
// synthetic disposable database once per task, while its host date is valid.
func runDoingFreeze(args []string) error {
	f := flag.NewFlagSet("doing-freeze", flag.ContinueOnError)
	suitePath := f.String("suite", "testdata/phase2_5/doing-independent/suite.json", "synthetic suite")
	dsn := f.String("database-url", "", "empty disposable localhost database")
	output := f.String("output", "/var/tmp/pcas-v2b-freeze/snapshot.json", "snapshot outside repositories")
	revision := f.String("revision", "unknown", "capture revision")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected freeze arguments")
	}
	s, err := doing.Load(*suitePath)
	if err != nil {
		return err
	}
	if !s.Synthetic {
		return fmt.Errorf("freeze accepts synthetic data only")
	}
	if err := outsideRepository(*output); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		return err
	}
	date := time.Now().UTC().Format("2006-01-02")
	if s.AsOf[:10] != date {
		return fmt.Errorf("freeze requires suite date equal to host UTC date")
	}
	raw, err := os.ReadFile(*suitePath)
	if err != nil {
		return err
	}
	snapshot := doing.ContextSnapshot{Version: 1, Synthetic: true, SuiteSHA: doing.SHA(string(raw)), AsOf: s.AsOf, HostDate: date, Revision: *revision, StartedAt: time.Now().UTC().Format(time.RFC3339), Entries: []doing.ContextEntry{}}
	observer := func(t doing.Task, e doing.Evidence, elapsed float64) error {
		if time.Now().UTC().Format("2006-01-02") != date {
			return fmt.Errorf("capture crossed UTC midnight")
		}
		snapshot.Entries = append(snapshot.Entries, doing.ContextEntry{Task: t.ID, RequestSHA: doing.SHA(t.Request), Text: e.Text, ContextSHA: doing.SHA(e.Text), ContextChars: utf8.RuneCountInString(e.Text), CaptureMS: elapsed, CaptureCalls: e.CaptureCalls})
		return nil
	}
	// The ordinary live current provider is reused exactly; fake judgments are
	// only plumbing. No answer, gold, judge instruction or method label goes into
	// the snapshot. The observer runs under the current provider's serial lock.
	err = runDoingObserved([]string{"-suite", *suitePath, "-database-url", *dsn, "-output", *output + ".fake-flow", "-revision", *revision, "-fake", "-methods=current", "-repeats=1", "-workers=1"}, observer)
	if err != nil {
		return err
	}
	snapshot.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	if snapshot.CompletedAt[:10] != date || len(snapshot.Entries) != len(s.Tasks) {
		return fmt.Errorf("incomplete or cross-date snapshot")
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool { return snapshot.Entries[i].Task < snapshot.Entries[j].Task })
	return doing.WriteJSON(*output, snapshot)
}
