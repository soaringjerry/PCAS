package interpstore

// Done restoration markers belong to the comparison namespace, so queue
// cleanup for unrelated processing cannot discard a comparison exemption.
// Recognize the original spelling too for restores made before this fix.
func restoredMemorySQL(rule string) string {
	// A user's restoration of this memory version survives prompt upgrades.
	// Recognize both historical marker spellings, and only this record version.
	return `EXISTS(SELECT 1 FROM background_markers restored WHERE restored.owner_id=cl.owner_id AND restored.record_id=cl.id AND restored.record_version=r.version AND ` + rule + `::int>=1 AND (restored.stage LIKE 'memory.compare:%:restored:%' OR restored.stage LIKE 'memory.compare_restored:%'))`
}
