package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/russellhaering/autoswe/pkg/tools"
)

const (
	webFetchDefaultTimeout = 30 * time.Second
	webFetchMaxBytes       = 2 << 20 // 2 MiB
	webFetchUserAgent      = "autoswe/0.1 (+https://github.com/russellhaering/autoswe)"
)

type webFetchArgs struct {
	URL     string `json:"url"`
	Timeout int    `json:"timeout,omitempty"` // seconds
}

type webFetchTool struct {
	client *http.Client
}

func (webFetchTool) Name() string { return "web_fetch" }

func (webFetchTool) Description() string {
	return "Fetch a URL over HTTPS and return its content. HTML is converted to a plain-text reading with link references appended; other text content is returned as-is. Response size is capped at 2 MiB."
}

func (webFetchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "Absolute http(s) URL to fetch."},
    "timeout": {"type": "integer", "minimum": 1, "maximum": 120, "description": "Per-request timeout in seconds. Defaults to 30."}
  },
  "required": ["url"]
}`)
}

func (webFetchTool) Effects() []tools.Effect {
	return []tools.Effect{tools.EffectNetwork}
}

func (t webFetchTool) Run(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	var a webFetchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.URL == "" {
		return tools.Result{IsError: true, Content: "url is required"}, nil
	}
	u, err := url.Parse(a.URL)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid url: %v", err)}, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return tools.Result{IsError: true, Content: fmt.Sprintf("unsupported scheme: %s (only http/https)", u.Scheme)}, nil
	}

	timeout := webFetchDefaultTimeout
	if a.Timeout > 0 {
		timeout = time.Duration(a.Timeout) * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("build request: %v", err)}, nil
	}
	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.5")

	client := t.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("fetch %s: %v", a.URL, err)}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(webFetchMaxBytes+1)))
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("read %s: %v", a.URL, err)}, nil
	}
	truncated := len(body) > webFetchMaxBytes
	if truncated {
		body = body[:webFetchMaxBytes]
	}

	if resp.StatusCode >= 400 {
		return tools.Result{
			IsError: true,
			Content: fmt.Sprintf("HTTP %d %s\n%s", resp.StatusCode, resp.Status, summarizeBody(string(body))),
		}, nil
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	var text string
	if strings.Contains(ct, "html") || strings.Contains(ct, "xml") {
		text = htmlToText(string(body))
	} else {
		text = string(body)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\nStatus: %d %s\nContent-Type: %s\n\n",
		resp.Request.URL.String(), resp.StatusCode, resp.Status, resp.Header.Get("Content-Type"))
	b.WriteString(text)
	if truncated {
		b.WriteString("\n\n[response truncated at 2 MiB]")
	}
	return tools.Result{Content: b.String()}, nil
}

// htmlToText renders an HTML document as plain text with link references
// appended. <script> and <style> contents are dropped. Whitespace is
// collapsed. Anchor texts are preserved inline; the URL targets are
// listed in a numbered footer.
func htmlToText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var (
		out       strings.Builder
		links     []linkRef
		skipDepth int
		linkURL   string
		linkBuf   strings.Builder
		inAnchor  bool
	)

	flushLink := func() {
		if !inAnchor {
			return
		}
		inAnchor = false
		text := strings.TrimSpace(collapseSpaces(linkBuf.String()))
		linkBuf.Reset()
		if text == "" {
			text = linkURL
		}
		if linkURL == "" || linkURL == text {
			out.WriteString(text)
			return
		}
		idx := len(links) + 1
		links = append(links, linkRef{Index: idx, URL: linkURL, Text: text})
		fmt.Fprintf(&out, "%s [%d]", text, idx)
		linkURL = ""
	}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := z.TagName()
			name := string(tn)
			switch name {
			case "script", "style", "noscript":
				if tt == html.StartTagToken {
					skipDepth++
				}
			case "a":
				flushLink()
				if hasAttr {
					for {
						k, v, more := z.TagAttr()
						if string(k) == "href" {
							linkURL = string(v)
						}
						if !more {
							break
						}
					}
				}
				inAnchor = true
			case "br":
				if skipDepth == 0 {
					out.WriteByte('\n')
				}
			case "p", "div", "li", "tr", "section", "article", "header", "footer",
				"h1", "h2", "h3", "h4", "h5", "h6":
				if skipDepth == 0 {
					out.WriteString("\n\n")
				}
			}
		case html.EndTagToken:
			tn, _ := z.TagName()
			name := string(tn)
			switch name {
			case "script", "style", "noscript":
				if skipDepth > 0 {
					skipDepth--
				}
			case "a":
				flushLink()
			case "p", "div", "li", "tr", "section", "article", "header", "footer",
				"h1", "h2", "h3", "h4", "h5", "h6":
				if skipDepth == 0 {
					out.WriteByte('\n')
				}
			}
		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			text := string(z.Text())
			if inAnchor {
				linkBuf.WriteString(text)
			} else {
				out.WriteString(text)
			}
		}
	}
	flushLink()

	body := collapseBlankLines(collapseSpaces(out.String()))
	body = strings.TrimSpace(body)

	if len(links) > 0 {
		var l strings.Builder
		l.WriteString("\n\nLinks:\n")
		for _, ref := range links {
			fmt.Fprintf(&l, "[%d] %s\n", ref.Index, ref.URL)
		}
		body += l.String()
	}
	return body
}

type linkRef struct {
	Index int
	URL   string
	Text  string
}

func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t':
			if lastSpace {
				continue
			}
			lastSpace = true
			b.WriteByte(' ')
		case '\n':
			b.WriteByte('\n')
			lastSpace = false
		default:
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return b.String()
}

func collapseBlankLines(s string) string {
	out := s
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return out
}

func summarizeBody(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512] + "…"
	}
	return s
}

// WebFetch is the built-in web_fetch tool.
var WebFetch tools.Tool = webFetchTool{}
