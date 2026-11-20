// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWikiProjectNavUsesSharedLinkStyles(t *testing.T) {
	if !strings.Contains(wikiTemplate, `<nav class="wiki-project-nav" aria-label="Project" id="project-links">`) {
		t.Fatal("wiki project links do not opt into the shared navigation styles")
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "site", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	styles := strings.ReplaceAll(string(css), "\r\n", "\n")
	for _, rule := range []string{
		".site-nav,\n.wiki-project-nav {",
		".site-nav > a,\n.wiki-project-nav > a {",
		".site-nav > a:hover,\n.wiki-project-nav > a:hover {",
		".wiki-project-nav > a:focus-visible {",
		".wiki-project-nav > a:active {",
	} {
		if !strings.Contains(styles, rule) {
			t.Errorf("shared navigation stylesheet lacks %q", rule)
		}
	}
	if strings.Contains(wikiTemplate, `<nav class="site-nav"`) {
		t.Error("wiki must not inherit the landing menu's open-state rules")
	}
}

func TestWikiHeaderTargetsAndAlignment(t *testing.T) {
	for _, selector := range []string{".wiki-theme-btn-bar", ".wiki-links-toggle", ".wiki-sheet-close"} {
		rule := regexp.MustCompile(regexp.QuoteMeta(selector) + `\s*\{([^}]+)\}`).FindStringSubmatch(wikiTemplate)
		if len(rule) < 2 || !strings.Contains(rule[1], "width: 2.75rem;") || !strings.Contains(rule[1], "height: 2.75rem;") {
			t.Errorf("%s must retain a 44px target without enlarging its icon", selector)
		}
	}
	if !strings.Contains(wikiTemplate, ".masthead .wordmark {\n        min-height: 2.75rem;") {
		t.Error("the logo link needs a 44px target without enlarging the logo")
	}
	if !strings.Contains(wikiTemplate, "flex-direction: column;\n          align-items: stretch;") {
		t.Error("mobile links must span the sheet's width")
	}
	if !strings.Contains(wikiTemplate, ".wiki-project-nav {\n        margin-left: auto;") {
		t.Error("desktop project links must sit beside the theme control, not float in the center")
	}
	if !strings.Contains(wikiTemplate, "margin-left: 0;\n          gap: 0;") {
		t.Error("the mobile sheet must reset desktop auto-margin and link gaps")
	}
}
