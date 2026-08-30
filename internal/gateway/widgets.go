package gateway

// Shared helper for pruning stale metric-key references out of saved
// dashboard/diagnostics widgets (dashboard_widgets.widgets JSON). Used both
// by DeleteGateway (manager.go, cleans up refs to the one gateway just
// deleted, across all users) and by internal/api's dashboard handlers
// (self-heals refs to gateways that were already gone before that fix
// existed, one user/page row at a time).

// FilterGraphRefs removes any widget "graphs" entry (and matching
// "graphColors" key) for which stale returns true, across all given
// widgets. Widgets are mutated in place. Returns whether anything changed.
func FilterGraphRefs(widgets []map[string]interface{}, stale func(key string) bool) bool {
	changed := false
	for _, w := range widgets {
		if graphs, ok := w["graphs"].([]interface{}); ok {
			// Filter into a fresh slice — never reuse graphs' backing array
			// in place, since a caller could be holding onto the original
			// slice elsewhere.
			filtered := make([]interface{}, 0, len(graphs))
			for _, g := range graphs {
				if gs, ok := g.(string); ok && stale(gs) {
					changed = true
					continue
				}
				filtered = append(filtered, g)
			}
			w["graphs"] = filtered
		}
		if colors, ok := w["graphColors"].(map[string]interface{}); ok {
			for key := range colors {
				if stale(key) {
					delete(colors, key)
					changed = true
				}
			}
		}
	}
	return changed
}
