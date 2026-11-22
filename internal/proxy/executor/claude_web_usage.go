// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package executor

// claude.ai's web lane does not report token counts, so usage is estimated
// locally with the same estimator gemini-web uses (geminiWebBuildUsage:
// chars-per-token with a CJK correction). If a future lane event carries real
// counts, read them beside claudeWebEventText and prefer them here.
func claudeWebUsage(prompt, answer string) map[string]any {
	return geminiWebBuildUsage(prompt, answer, "")
}
