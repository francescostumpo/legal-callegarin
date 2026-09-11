package articles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
)

const maxDocumentLinks = 256

type documentNode struct {
	Type    string            `json:"type"`
	Attrs   *nodeAttrs        `json:"attrs,omitempty"`
	Content []json.RawMessage `json:"content,omitempty"`
	Text    string            `json:"text,omitempty"`
	Marks   []documentMark    `json:"marks,omitempty"`
}
type nodeAttrs struct {
	Level int `json:"level"`
}
type documentMark struct {
	Type  string     `json:"type"`
	Attrs *markAttrs `json:"attrs,omitempty"`
}
type markAttrs struct {
	Href   string  `json:"href"`
	Target *string `json:"target,omitempty"`
	Rel    *string `json:"rel,omitempty"`
	Class  *string `json:"class,omitempty"`
}
type compileState struct {
	nodes, links int
	plain        strings.Builder
}

func CompileDocument(schemaVersion int, raw json.RawMessage) (Body, error) {
	if schemaVersion != 1 {
		return Body{}, validationError("body schema version must be 1")
	}
	if len(raw) == 0 || len(raw) > maxDocumentBytes || !json.Valid(raw) {
		return Body{}, validationError("body document must be valid JSON of at most 512 KiB")
	}
	var root documentNode
	if err := decodeExact(raw, &root); err != nil {
		return Body{}, validationError("body document contains invalid or unknown fields")
	}
	state := &compileState{}
	markup, err := compileNode(root, 1, "", state)
	if err != nil {
		return Body{}, err
	}
	if root.Type != "doc" || strings.TrimSpace(state.plain.String()) == "" {
		return Body{}, validationError("body document must be a non-empty doc")
	}
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "h2", "h3", "ul", "ol", "li", "strong", "em", "a", "blockquote")
	policy.AllowAttrs("href", "rel", "target").OnElements("a")
	clean := policy.Sanitize(markup)
	if clean != markup {
		return Body{}, validationError("compiled body failed sanitization")
	}
	plain := strings.TrimSpace(state.plain.String())
	if len(clean) > maxHTMLBytes || len(plain) > maxPlainTextBytes {
		return Body{}, validationError("compiled body exceeds size limit")
	}
	canonical := append(json.RawMessage(nil), raw...)
	return Body{SchemaVersion: 1, Document: canonical, HTML: clean, PlainText: plain}, nil
}

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func compileNode(node documentNode, depth int, parent string, state *compileState) (string, error) {
	state.nodes++
	if depth > maxDocumentDepth || state.nodes > maxDocumentNodes {
		return "", validationError("body document exceeds structural limits")
	}
	children := func(allowed string) (string, error) {
		var b strings.Builder
		for _, raw := range node.Content {
			var child documentNode
			if err := decodeExact(raw, &child); err != nil {
				return "", validationError("body node contains invalid fields")
			}
			out, err := compileNode(child, depth+1, allowed, state)
			if err != nil {
				return "", err
			}
			b.WriteString(out)
		}
		return b.String(), nil
	}
	switch node.Type {
	case "doc":
		if parent != "" || node.Attrs != nil || node.Text != "" || len(node.Marks) > 0 {
			return "", validationError("invalid doc node")
		}
		return children("block")
	case "paragraph":
		if parent != "block" && parent != "listItem" {
			return "", validationError("paragraph in invalid position")
		}
		if node.Attrs != nil || node.Text != "" || len(node.Marks) > 0 {
			return "", validationError("invalid paragraph")
		}
		c, e := children("inline")
		state.plain.WriteString("\n\n")
		return "<p>" + c + "</p>", e
	case "heading":
		if parent != "block" || node.Attrs == nil || (node.Attrs.Level != 2 && node.Attrs.Level != 3) || node.Text != "" || len(node.Marks) > 0 {
			return "", validationError("invalid heading")
		}
		c, e := children("inline")
		state.plain.WriteString("\n\n")
		tag := fmt.Sprintf("h%d", node.Attrs.Level)
		return "<" + tag + ">" + c + "</" + tag + ">", e
	case "bulletList", "orderedList":
		if parent != "block" && parent != "listItem" || node.Attrs != nil || node.Text != "" || len(node.Marks) > 0 {
			return "", validationError("invalid list")
		}
		c, e := children("list")
		tag := "ul"
		if node.Type == "orderedList" {
			tag = "ol"
		}
		state.plain.WriteString("\n")
		return "<" + tag + ">" + c + "</" + tag + ">", e
	case "listItem":
		if parent != "list" || node.Attrs != nil || node.Text != "" || len(node.Marks) > 0 || len(node.Content) == 0 {
			return "", validationError("invalid list item")
		}
		c, e := children("listItem")
		state.plain.WriteString("\n")
		return "<li>" + c + "</li>", e
	case "blockquote":
		if parent != "block" || node.Attrs != nil || node.Text != "" || len(node.Marks) > 0 {
			return "", validationError("invalid blockquote")
		}
		c, e := children("block")
		state.plain.WriteString("\n\n")
		return "<blockquote>" + c + "</blockquote>", e
	case "text":
		if parent != "inline" || node.Attrs != nil || len(node.Content) > 0 || node.Text == "" || !utf8.ValidString(node.Text) {
			return "", validationError("invalid text node")
		}
		state.plain.WriteString(node.Text)
		out := html.EscapeString(node.Text)
		seen := map[string]bool{}
		for i := len(node.Marks) - 1; i >= 0; i-- {
			mark := node.Marks[i]
			if seen[mark.Type] {
				return "", validationError("duplicate mark")
			}
			seen[mark.Type] = true
			switch mark.Type {
			case "bold":
				if mark.Attrs != nil {
					return "", validationError("bold attributes are forbidden")
				}
				out = "<strong>" + out + "</strong>"
			case "italic":
				if mark.Attrs != nil {
					return "", validationError("italic attributes are forbidden")
				}
				out = "<em>" + out + "</em>"
			case "link":
				state.links++
				if state.links > maxDocumentLinks || mark.Attrs == nil {
					return "", validationError("invalid link mark")
				}
				href, external, err := safeHref(mark.Attrs.Href)
				if err != nil {
					return "", err
				}
				attrs := " href=\"" + html.EscapeString(href) + "\""
				if external {
					attrs += " rel=\"noopener noreferrer\""
					if mark.Attrs.Target != nil && *mark.Attrs.Target == "_blank" {
						attrs += " target=\"_blank\""
					}
				}
				out = "<a" + attrs + ">" + out + "</a>"
			default:
				return "", validationError("unknown text mark")
			}
		}
		return out, nil
	default:
		return "", validationError("unknown body node")
	}
}

func safeHref(value string) (string, bool, error) {
	if value == "" || utf8.RuneCountInString(value) > maxLinkLength || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return "", false, validationError("unsafe link")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", false, validationError("unsafe link")
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil {
		return "", false, validationError("unsafe link")
	}
	if u.Scheme == "" {
		if u.Host != "" {
			return "", false, validationError("unsafe link")
		}
		return u.String(), false, nil
	}
	if u.Scheme == "mailto" {
		if u.Opaque == "" && u.Path == "" {
			return "", false, validationError("unsafe link")
		}
		return u.String(), false, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return "", false, validationError("unsafe link")
	}
	return u.String(), true, nil
}
