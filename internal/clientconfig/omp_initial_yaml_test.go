// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package clientconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

type ompProviderEntry struct {
	BaseURL string `yaml:"baseUrl"`
	APIKey  string `yaml:"apiKey"`
	API     string `yaml:"api"`
}

// TestOmpInitialYAML proves a first-ever OMP connect lands the full nested
// providers.alyysha object on a file that has no YAML document to splice
// into — the empty 0-byte models.yml a fresh oh-my-pi install leaves behind
// (issue #7). Written values are read back through the decoder, not string
// matching, so any structurally wrong success fails here.
func TestOmpInitialYAML(t *testing.T) {
	cases := []struct {
		name, src string
		valid     bool
	}{
		{"empty", "", true},
		{"spaces", "   ", true},
		{"newline", "\n", true},
		{"BOM only", "\ufeff", true},
		{"BOM empty mapping", "\ufeff{}\n", true},
		{"empty mapping", "{}\n", true},
		{"comment-only", "# owner comment\n", true},
		{"scalar root", "plain scalar\n", false},
		{"sequence root", "- item\n", false},
		{"prose root", "This file is not YAML config\nproviders:\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := ompPaths(home).Files[0].Path
			writeFixture(t, path, tc.src)
			a, err := Resolve("omp")
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.Apply(Options{Home: home, BaseURL: "http://127.0.0.1:20130", APIKey: "fixture-key", Model: "fixture-model"})
			if !tc.valid {
				want := filepath.Base(path) + " does not look like a YAML mapping (line 1). Delete or fix the file, or let this connection recreate it."
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("want actionable non-mapping error naming the file, got %v", err)
				}
				if !errors.Is(err, ErrUnsupportedStructure) {
					t.Fatalf("error classification lost: %v", err)
				}
				if got := readFixture(t, path); got != tc.src {
					t.Fatalf("rejected file was modified:\n got %q\nwant %q", got, tc.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("Apply on %q: %v", tc.name, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Providers map[string]ompProviderEntry `yaml:"providers"`
			}
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatalf("written YAML does not decode: %v\n%s", err, data)
			}
			entry, ok := doc.Providers["alyysha"]
			if !ok {
				t.Fatalf("providers.alyysha missing from written file:\n%s", data)
			}
			if entry.BaseURL != "http://127.0.0.1:20130/v1" || entry.APIKey != "fixture-key" || entry.API != "openai-completions" {
				t.Fatalf("written provider entry has wrong values: %+v", entry)
			}
			if strings.Contains(tc.src, "# owner comment") && !strings.Contains(string(data), "# owner comment") {
				t.Error("comment-only file lost its comment on creation")
			}
		})
	}
}

// The guarded semantics from the first attempt must not move: deleting a flow
// collection is still refused, and the file is left byte-identical.
func TestOmpCreatedFlowEntryResetStillRefused(t *testing.T) {
	home := t.TempDir()
	path := ompPaths(home).Files[0].Path
	writeFixture(t, path, "")
	a, err := Resolve("omp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(Options{Home: home, BaseURL: "http://127.0.0.1:20130", APIKey: "fixture-key", Model: "fixture-model"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	before := readFixture(t, path)
	_, resetErr := a.Reset(Options{Home: home})
	if resetErr == nil || !strings.Contains(resetErr.Error(), "flow collection") {
		t.Fatalf("Reset on a flow-collection entry must stay refused, got %v", resetErr)
	}
	if after := readFixture(t, path); after != before {
		t.Fatalf("refused Reset changed the file:\n before %q\n after %q", before, after)
	}
}
