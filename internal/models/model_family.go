// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package models

import (
	"regexp"
	"strings"
)

// Model family (N9): collapses a model id to the family every version of that
// model shares, across providers. The dashboard groups families into auto
// combos, the combo editor flags a ladder that crosses families, and the
// dispatch records each family boundary a fallback crosses — all three read
// the one function so they cannot disagree about what a family is.
//
// versionToken matches a generation marker: "4.6", "3.1", "2", "v2.5".
var versionToken = regexp.MustCompile(`^v?\d+(\.\d+)*$`)

// letterDigitToken matches a mid-name generation marker such as kimi-"k2.6" or
// minimax-"m3". It is only stripped when it is not the first token: a leading
// token is the model line itself, so "o1" and "o3-mini" survive intact.
var letterDigitToken = regexp.MustCompile(`^[a-z]\d+(\.\d+)*$`)

// familyFlagWords are capability/lifecycle markers, not identity: stripping
// them lets "gemini-3.1-flash-preview" and "gemini-3-flash-preview" meet in the
// family "gemini-flash".
var familyFlagWords = map[string]bool{
	"preview": true, "exp": true, "experimental": true,
	"beta": true, "latest": true, "free": true, "auto": true,
}

// ModelFamily collapses a model id to its family.
func ModelFamily(modelID string) string {
	lower := strings.ToLower(modelID)

	// Drop a free-tier marker ("model:free") so free and paid versions group.
	lower = strings.TrimSuffix(lower, ":free")

	parts := strings.Split(lower, "-")
	kept := make([]string, 0, len(parts))
	for i, p := range parts {
		if p == "" {
			continue
		}
		if versionToken.MatchString(p) || familyFlagWords[p] {
			continue
		}
		if i > 0 && letterDigitToken.MatchString(p) {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		// Nothing identifiable (e.g. a pure version id); keep the original so
		// the model is not silently merged into an empty family.
		return lower
	}
	return strings.Join(kept, "-")
}

// ComboFamilies walks a combo's "alias/model" member list and returns the
// distinct families in ladder order, plus whether the ladder crosses a
// family boundary. Unresolvable entries (no "/") contribute nothing: a
// nested combo name has no family of its own.
func ComboFamilies(members []string) (families []string, cross bool) {
	for _, entry := range members {
		slash := strings.LastIndex(entry, "/")
		if slash < 0 || slash == len(entry)-1 {
			continue
		}
		f := ModelFamily(entry[slash+1:])
		if f == "" {
			continue
		}
		if len(families) == 0 || families[len(families)-1] != f {
			families = append(families, f)
		}
	}
	// cross compares family LINES, not whole families: gpt-4.1 and
	// gpt-4.1-mini share the gpt line (same tokenizer, same prompt cache),
	// while gpt -> claude-sonnet is the crossing that changes everything.
	// A mixed ladder is one crossing however many steps it spans.
	if len(families) > 1 {
		lines := map[string]bool{}
		for _, f := range families {
			lines[familyLine(f)] = true
		}
		cross = len(lines) > 1
	}
	return families, cross
}

// familyLine is the model line a family belongs to: its first token, which
// the family function never strips for a leading token.
func familyLine(family string) string {
	if idx := strings.Index(family, "-"); idx > 0 {
		return family[:idx]
	}
	return family
}
