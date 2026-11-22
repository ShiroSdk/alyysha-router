// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	htmltoken "golang.org/x/net/html"
)

const rawBadge = `<a href="https://trendshift.io/repositories/348263?utm_source=trendshift-badge&amp;utm_medium=badge" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/trendshift/repositories/348263/daily?language=Go" alt="Alyysha Router | Trendshift" width="250" height="55"/></a>`

func TestRenderRawHTMLImage(t *testing.T) {
	for _, renderer := range []string{"readme", "wiki"} {
		t.Run(renderer, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "page.md")
			writeFixture(t, input, "# Providers\n\n"+rawBadge+"\n")
			out := filepath.Join(root, "site")
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			var rendered string
			if renderer == "readme" {
				body, err := renderMarkdown(input, out, defaultRepo)
				if err != nil {
					t.Fatal(err)
				}
				rendered = string(body)
			} else {
				page, err := renderWikiPage(input, out)
				if err != nil {
					t.Fatal(err)
				}
				rendered = string(page.Body)
			}
			for _, attr := range []string{`loading="lazy"`, `decoding="async"`, `width="250"`, `height="55"`, `alt="Alyysha Router | Trendshift"`} {
				if !strings.Contains(rendered, attr) {
					t.Errorf("missing %s in rendered raw image: %s", attr, rendered)
				}
			}
			anchorEnd := strings.Index(rawBadge, ">") + 1
			if !strings.Contains(rendered, rawBadge[:anchorEnd]) || !strings.Contains(rendered, "</a>") {
				t.Errorf("badge link was changed: %s", rendered)
			}
			if !strings.Contains(rendered, `src="https://trendshift.io/api/badge/trendshift/repositories/348263/daily?language=Go"`) {
				t.Errorf("badge source changed: %s", rendered)
			}
		})
	}
}

func TestNormalizeImages(t *testing.T) {
	tests := []struct {
		name, markup string
	}{
		{"raw badge", rawBadge},
		{"single quotes and multiline", "<p>before</p><IMG\nsrc='x.png' alt='a &amp; b' width='32' height='24'><p>after</p>"},
		{"existing policy", `<img src="x.png" loading="lazy" decoding="async">`},
		{"override eager policy", `<img src="x.png" loading="eager" decoding="sync">`},
		{"duplicate attributes", `<img src="x.png" loading="eager" loading="lazy" decoding="sync" decoding="async">`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeImages(tt.markup)
			if second := normalizeImages(got); second != got {
				t.Errorf("not idempotent: first %q, second %q", got, second)
			}
			z := htmltoken.NewTokenizer(strings.NewReader(got))
			for {
				typeOf := z.Next()
				if typeOf == htmltoken.ErrorToken {
					if z.Err() != io.EOF {
						t.Fatal(z.Err())
					}
					break
				}
				token := z.Token()
				if token.Data != "img" {
					continue
				}
				counts := map[string]int{}
				for _, attr := range token.Attr {
					counts[attr.Key]++
					if attr.Key == "loading" && attr.Val != "lazy" || attr.Key == "decoding" && attr.Val != "async" {
						t.Errorf("incorrect image policy: %+v", attr)
					}
				}
				for _, name := range []string{"loading", "decoding"} {
					if counts[name] != 1 {
						t.Errorf("%s count=%d, want 1", name, counts[name])
					}
				}
			}
		})
	}
	for _, markup := range []string{
		`<!-- <img src="comment.png"> -->`,
		`<script>const sample = '<img src="script.png">';</script>`,
		`<code>&lt;img src="example.png"&gt;</code>`,
		`<textarea><img src="text.png"></textarea>`,
		`<div title='<img src="attribute.png">'>plain</div>`,
		`<img src="x.png" loading="lazy" decoding="async"/>`,
	} {
		t.Run(fmt.Sprintf("unchanged %s", markup), func(t *testing.T) {
			if got := normalizeImages(markup); got != markup {
				t.Errorf("unrelated/compliant markup changed: %q", got)
			}
		})
	}
}
