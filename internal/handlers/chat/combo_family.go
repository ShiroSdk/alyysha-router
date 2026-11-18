// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package chat

import (
	"strings"

	"9routerx/proxy/internal/models"
)

// Model-family fallback grouping (N9): a combo ladder that crosses a model
// family — gpt to claude, claude to gemini — changes tokenizer, prompt cache
// and capability set mid-fallback. The ladder stays legal, but every crossing
// is recorded on the routing-explain panel the moment a dispatch walks it, so
// an unexpected mix is visible where the operator is looking when it fires.
// The family definition is the shared one in models, so the auto-combo
// grouping, the editor's cross-family flag and this record cannot disagree.

// recordFamilyBoundaries walks the resolved ladder and records one explain
// entry per family boundary, capped like every explain record by the ring.
// Nested combo names ("family-combo" without a slash) contribute nothing —
// they have no family of their own.
func recordFamilyBoundaries(comboName string, ladder []string) {
	if len(ladder) < 2 {
		return
	}
	prev := ""
	for _, entry := range ladder {
		slash := strings.LastIndex(entry, "/")
		if slash < 0 || slash == len(entry)-1 {
			continue
		}
		family := models.ModelFamily(entry[slash+1:])
		if family == "" {
			continue
		}
		// The boundary is the model LINE: gpt-4.1 -> gpt-4.1-mini rides the
		// same tokenizer and cache; gpt -> claude-sonnet does not.
		line := family
		if idx := strings.Index(family, "-"); idx > 0 {
			line = family[:idx]
		}
		if prev != "" && prev != line {
			RecordRoutingExplain(RoutingExplain{
				Provider: "family_fallback",
				Model:    comboName,
				Reason:   "combo ladder crosses model family: " + prev + " -> " + line,
				Outcome:  "attempted",
			})
		}
		prev = line
	}
}
