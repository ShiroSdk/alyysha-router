// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package clientconfig

import (
	json "encoding/json/v2"
	"path/filepath"
)

// TODO_OWNER: the Hermes config schema (multi-role delegation/auxiliary
// fields) could not be verified against a real Hermes installation during
// development. The adapter writes endpoint + credential only and preserves
// every other field in the YAML document. A `--config` flag on the CLI lets
// the operator point at a non-default path.

type hermesAdapter struct {
	baseAdapter
}

func (a hermesAdapter) Paths(opts Options) (Paths, error) {
	home := opts.Home
	if home == "" {
		h, err := homeDir()
		if err != nil {
			return Paths{}, err
		}
		home = h
	}
	// TODO_OWNER: verify the actual config path from a real Hermes install.
	return hermesPaths(home), nil
}

// hermesPaths is the shared path builder — exported for the paths test.
func hermesPaths(home string) Paths {
	dir := filepath.Join(home, ".hermes")
	return Paths{
		Dir:   dir,
		Files: []TargetFile{{Path: filepath.Join(dir, "config.yaml"), Dir: dir, Format: FormatYAML, Purpose: "config"}},
	}
}

func (a hermesAdapter) Plan(opts Options) ([]FilePlan, error) {
	paths, err := a.Paths(opts)
	if err != nil {
		return nil, err
	}
	// The llm block is written as one ValueJSON edit: three edits sharing the
	// `llm` table would each append a `llm:` header, producing duplicate keys
	// the YAML parser rejects. A single spliced object merges into any
	// existing table in one pass.
	llmBlock := map[string]any{"api_base": opts.BaseURL, "api_key": opts.APIKey}
	if opts.Model != "" {
		llmBlock["model"] = opts.Model
	}
	llmJSON, err := json.Marshal(llmBlock, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	edits := []Edit{
		{KeyPath: []string{"llm"}, ValueJSON: string(llmJSON), CreateIfMissing: true},
	}
	return []FilePlan{{File: paths.Files[0], Edits: edits}}, nil
}

func (a hermesAdapter) Apply(opts Options) (Result, error) {
	plans, err := a.Plan(opts)
	if err != nil {
		return Result{}, err
	}
	if err := AssertNoForbiddenEdits(plans); err != nil {
		return Result{}, err
	}
	return applyPlans(plans, nil)
}

func (a hermesAdapter) Reset(opts Options) (Result, error) {
	paths, err := a.Paths(opts)
	if err != nil {
		return Result{}, err
	}
	edits := []Edit{
		{KeyPath: []string{"llm", "api_base"}, Delete: true},
		{KeyPath: []string{"llm", "api_key"}, Delete: true},
	}
	return applyPlans([]FilePlan{{File: paths.Files[0], Edits: edits}}, nil)
}
