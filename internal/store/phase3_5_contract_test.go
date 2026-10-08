package store

// Frozen from phase3_5 §4, phase2_6 G1–G7 and process §1–2 BEFORE
// reading implementations or skeleton PRs. Adapter details may change; these
// expected outcomes must not be rewritten to match current behavior.
var phase35FrozenRules = []struct{ id, expected string }{
 {"A1", "range reads mix appointments, deadlines, recurring occurrences and timed todos in local chronological order; upcoming includes the existing three-day boundary"},
 {"A2", "unclear dates never enter the timed line; they retain original speech at the end of upcoming"},
 {"A3", "overdue unknown completion is visible; complete updates its source memory and creates no todo"},
 {"A4", "recurrence expands only inside the requested range, preserving wall clock through DST and timezone changes, without materialization"},
 {"B1", "direct explicit unqualified personal todos AND ideas auto-create; reported, reserved, AI suggestions and uncertain claims remain reasoned candidates"},
 {"B2", "imported, historical and non-owner content never creates current work"},
 {"B3", "creation has actor/evidence provenance and undoable receipt; undo removes work/provenance and retains original speech"},
 {"B4", "replay including replay after undo never recreates the same extracted work"},
 {"B5", "overflow remains candidates and has observable counts"},
 {"B6", "autoAccept writes are ignored and its old value cannot gate creation"},
 {"C1", "goal plus deadline OR multiple steps OR multiple days can create project; undo detaches its todos; single-step speech creates no project"},
 {"C2", "only topic with enough current memories AND goal AND deadline creates project, links work_item_id, reports and undoes without touching memories; daily overflow remains next day"},
 {"C3", "model decides existing-project equivalence, including different names; no semantic keyword matching"},
 {"C4", "automatic projects enter project-handover scheduling"},
 {"D1", "untimed non-urgent active todos appear in in-progress ordered by latest progress"},
 {"D2", "display cap has stated basis and omitted work stays available in projects"},
 {"G1", "every bound has rationale and observable overflow destination"},
 {"G2", "failed/invalid/exhausted model output preserves good state, remains pending and records reason/count"},
 {"G3", "successful model result followed by failed write retries persistence without another model call"},
 {"G4", "single memory change schedules only affected scope"},
 {"G5", "model supplies semantic decisions, code validates fields; uncertainty remains visible"},
 {"G6", "all acceptance GETs preserve persistent state"},
 {"G7", "each background stage has independent bounded model budget and stated per-memory call ceiling"},
}

var phase35FrozenSequences = []struct{ name, operations, expected string }{
 {"continuous", "create A; create B; undo B; undo A", "initial work and evidence state restored; speech retained"},
 {"jump", "create A; create unrelated B; undo A", "A removed; B unchanged"},
 {"interleaved", "auto create A; user edits A; undo create A", "edited work not silently lost; conflict explicit and unrelated work preserved"},
 {"replay", "extract same speech twice; undo; extract same speech", "one creation before undo; zero after undo/replay"},
 {"concurrent", "two extractions and scheduling plus user request at once", "at most one work per evidence; no partial provenance; bounded calls"},
 {"boundary", "read across local midnight, day+3, DST forward/back and changed timezone", "only in-range occurrences; local recurring time preserved; three-day cutoff consistent"},
 {"cross-channel", "web creation; Telegram undo (same operation service)", "same state and preserved speech"},
 {"deleted", "create; delete evidence or item; undo", "no resurrection or unrelated deletion; explicit safe result"},
 {"project", "auto project with todo; undo project", "todo unassigned; topic disconnected; memories unchanged; empty project removed"},
 {"random", "seeded random independent creations/edits; undo all in reverse", "normalized business state exactly equals initial state"},
}

var phase35FrozenFixture = struct{ memories, groups, entities, largeGroups, largestGroup, deadlines, candidates, topics int }{5000,300,1000,10,1001,200,30,3}
