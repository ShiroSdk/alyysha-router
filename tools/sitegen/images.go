// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package main

import (
	"io"
	"strings"

	htmltoken "golang.org/x/net/html"
)

// normalizeImages applies the Markdown image policy to raw HTML too. Only
// image tags needing a change are serialized; links, comments, scripts and
// already-compliant markup retain their original bytes.
func normalizeImages(markup string) string {
	z := htmltoken.NewTokenizer(strings.NewReader(markup))
	var out strings.Builder
	out.Grow(len(markup))
	for {
		kind := z.Next()
		raw := string(z.Raw())
		if kind == htmltoken.ErrorToken {
			if z.Err() != io.EOF {
				return markup
			}
			out.WriteString(raw)
			return out.String()
		}
		if kind != htmltoken.StartTagToken && kind != htmltoken.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}
		token := z.Token()
		if token.Data != "img" {
			out.WriteString(raw)
			continue
		}
		if imagePolicyIsSet(token.Attr) {
			out.WriteString(raw)
			continue
		}
		attrs := make([]htmltoken.Attribute, 0, len(token.Attr)+2)
		for _, attr := range token.Attr {
			if attr.Key != "loading" && attr.Key != "decoding" {
				attrs = append(attrs, attr)
			}
		}
		token.Attr = append(attrs,
			htmltoken.Attribute{Key: "loading", Val: "lazy"},
			htmltoken.Attribute{Key: "decoding", Val: "async"},
		)
		out.WriteString(token.String())
	}
}

func imagePolicyIsSet(attrs []htmltoken.Attribute) bool {
	loading, decoding := 0, 0
	for _, attr := range attrs {
		switch attr.Key {
		case "loading":
			if attr.Val != "lazy" {
				return false
			}
			loading++
		case "decoding":
			if attr.Val != "async" {
				return false
			}
			decoding++
		}
	}
	return loading == 1 && decoding == 1
}
