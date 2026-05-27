package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.Strikethrough,
			extension.Table,
			extension.TaskList,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
		),
	)
	// UGCPolicy allows safe user-generated HTML: headings, lists, bold, italic,
	// links, code, blockquotes, tables — no scripts, iframes, or inline styles.
	pol = bluemonday.UGCPolicy()
)

// Render converts CommonMark markdown to sanitized HTML.
// Returns an empty string if src is empty or rendering fails.
func Render(src string) string {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return pol.Sanitize(buf.String())
}
