// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package clientconfig

import (
	"fmt"
	"strings"
)

// The connect patchers write exactly three things per client: the gateway
// endpoint (base_url / ANTHROPIC_BASE_URL / ...), the credential (api key /
// auth token), and at most the default model. Anything else — above all
// `hooks`, the CVE-2026-108593 anti-pattern where a connect helper injects
// command execution into the client's config — is refused before a byte is
// written. This guard walks the resolved plans and fails the whole connect
// when a forbidden key path appears.
//
// The adapters are structured (they build Edits from typed Options, not from
// a payload), so the guard is defence in depth: a future adapter mistake or
// a compromised patch table still cannot smuggle a hooks entry past connect.

// forbiddenPatchSegments are the key-path segments connect never writes.
// "hooks" covers every client dialect that spells it the same way
// (claude/codex/opencode); the exact match on the segment keeps a benign key
// like "hookah" safe.
var forbiddenPatchSegments = []string{"hooks"}

func AssertNoForbiddenEdits(plans []FilePlan) error {
	for _, plan := range plans {
		for _, edit := range plan.Edits {
			for _, segment := range edit.KeyPath {
				for _, forbidden := range forbiddenPatchSegments {
					if strings.EqualFold(strings.TrimSpace(segment), forbidden) {
						return fmt.Errorf(
							"connect refused: edit %q in %s carries a forbidden key (%s) — connect only writes the gateway endpoint, credential and model",
							strings.Join(edit.KeyPath, "."), plan.File.Path, forbidden)
					}
				}
			}
		}
	}
	return nil
}
