package articles

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileDocumentCanonicalizesAndSanitizes(t *testing.T) {
	document := json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Titolo & prova"}]},{"type":"paragraph","content":[{"type":"text","text":"Visita ","marks":[{"type":"bold"}]},{"type":"text","text":"il sito","marks":[{"type":"link","attrs":{"href":"https://example.test/path","target":"_blank","rel":"ignored"}}]}]}]}`)
	body, err := CompileDocument(1, document)
	if err != nil {
		t.Fatal(err)
	}
	if body.HTML != `<h2>Titolo &amp; prova</h2><p><strong>Visita </strong><a href="https://example.test/path" rel="noopener noreferrer" target="_blank">il sito</a></p>` {
		t.Fatalf("HTML = %q", body.HTML)
	}
	if body.PlainText != "Titolo & prova\n\nVisita il sito" {
		t.Fatalf("plain = %q", body.PlainText)
	}
	if err := body.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestCompileDocumentSupportsTheEntireRestrictedSchema(t *testing.T) {
	document := json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Sottotitolo","marks":[{"type":"italic"}]}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Punto"}]}]}]},{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Passo"}]}]}]},{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"Citazione"}]}]}]}`)
	body, err := CompileDocument(1, document)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"<h3><em>Sottotitolo</em></h3>", "<ul><li><p>Punto</p></li></ul>", "<ol><li><p>Passo</p></li></ol>", "<blockquote><p>Citazione</p></blockquote>"} {
		if !strings.Contains(body.HTML, fragment) {
			t.Fatalf("HTML %q lacks %q", body.HTML, fragment)
		}
	}
}

func TestCompileDocumentRejectsUnsafeOrUnknownInput(t *testing.T) {
	for name, document := range map[string]string{
		"javascript":        `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}]}`,
		"protocol relative": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"//evil.test"}}]}]}]}`,
		"data":              `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"data:text/html,x"}}]}]}]}`,
		"credentials":       `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://user@example.test"}}]}]}]}`,
		"unknown node":      `{"type":"doc","content":[{"type":"image","attrs":{"src":"x"}}]}`,
		"event attribute":   `{"type":"doc","content":[{"type":"paragraph","onclick":"evil","content":[{"type":"text","text":"x"}]}]}`,
		"style attribute":   `{"type":"doc","content":[{"type":"paragraph","style":"color:red","content":[{"type":"text","text":"x"}]}]}`,
		"unknown field":     `{"type":"doc","evil":true,"content":[]}`,
		"empty":             `{"type":"doc","content":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CompileDocument(1, json.RawMessage(document)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	long := strings.Repeat("a", maxLinkLength+1)
	doc := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"` + long + `"}}]}]}]}`)
	if _, err := CompileDocument(1, doc); err == nil {
		t.Fatal("expected long-link rejection")
	}
}

func TestCompileDocumentRejectsInvalidUTF8AndEmptyStructuralContainers(t *testing.T) {
	invalidUTF8 := append([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}]}]}`)...)
	if json.Valid(invalidUTF8) != true {
		t.Fatal("fixture must demonstrate encoding/json replacement behavior")
	}
	if _, err := CompileDocument(1, invalidUTF8); err == nil {
		t.Fatal("invalid UTF-8 document accepted")
	}
	for name, raw := range map[string]string{
		"empty doc":          `{"type":"doc","content":[]}`,
		"empty bullet list":  `{"type":"doc","content":[{"type":"bulletList","content":[]},{"type":"paragraph","content":[{"type":"text","text":"valido"}]}]}`,
		"empty ordered list": `{"type":"doc","content":[{"type":"orderedList","content":[]},{"type":"paragraph","content":[{"type":"text","text":"valido"}]}]}`,
		"empty list item":    `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[]}]},{"type":"paragraph","content":[{"type":"text","text":"valido"}]}]}`,
		"empty blockquote":   `{"type":"doc","content":[{"type":"blockquote","content":[]},{"type":"paragraph","content":[{"type":"text","text":"valido"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CompileDocument(1, json.RawMessage(raw)); err == nil {
				t.Fatal("empty structural container accepted")
			}
		})
	}
}

func TestCompileDocumentRejectsMalformedGrammarAndURLConfusion(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		schema int
		raw    string
	}{
		"malformed JSON":        {1, `{"type":"doc"`},
		"trailing JSON":         {1, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"ok"}]}]} {}`},
		"wrong schema":          {2, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"ok"}]}]}`},
		"unknown node attr":     {1, `{"type":"doc","content":[{"type":"heading","attrs":{"level":2,"class":"hero"},"content":[{"type":"text","text":"ok"}]}]}`},
		"unknown mark attr":     {1, linkDocument(`https://example.test`, `,"download":true`)},
		"unknown mark":          {1, markedTextDocument(`{"type":"underline"}`)},
		"duplicate mark":        {1, markedTextDocument(`{"type":"bold"},{"type":"bold"}`)},
		"block under paragraph": {1, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"paragraph","content":[{"type":"text","text":"no"}]}]}]}`},
		"text at document root": {1, `{"type":"doc","content":[{"type":"text","text":"no"}]}`},
		"list item at root":     {1, `{"type":"doc","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"no"}]}]}]}`},
		"heading in list item":  {1, `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"no"}]}]}]}]}`},
		"heading level one":     {1, `{"type":"doc","content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"no"}]}]}`},
		"scheme-relative URL":   {1, linkDocument(`//evil.test/path`, ``)},
		"backslash URL":         {1, linkDocument(`https:\\evil.test`, ``)},
		"credential URL":        {1, linkDocument(`https://user:pass@example.test`, ``)},
		"missing-host HTTP":     {1, linkDocument(`http:example.test`, ``)},
		"mixed-case script URL": {1, linkDocument(`JaVaScRiPt:alert(1)`, ``)},
		"control URL":           {1, linkDocument("https://example.test/\nnext", ``)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := CompileDocument(testCase.schema, json.RawMessage(testCase.raw)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestCompileDocumentEnforcesEveryStructuralAndSizeBound(t *testing.T) {
	t.Parallel()

	t.Run("document bytes", func(t *testing.T) {
		raw := json.RawMessage(strings.Repeat(" ", maxDocumentBytes+1))
		if _, err := CompileDocument(1, raw); err == nil {
			t.Fatal("oversized document accepted")
		}
	})
	t.Run("depth", func(t *testing.T) {
		leaf := map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "deep"}}}
		var node any = leaf
		for range maxDocumentDepth {
			node = map[string]any{"type": "blockquote", "content": []any{node}}
		}
		raw, err := json.Marshal(map[string]any{"type": "doc", "content": []any{node}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = CompileDocument(1, raw); err == nil {
			t.Fatal("over-depth document accepted")
		}
	})
	t.Run("node count", func(t *testing.T) {
		content := make([]any, maxDocumentNodes)
		for index := range content {
			content[index] = map[string]any{"type": "paragraph"}
		}
		raw, err := json.Marshal(map[string]any{"type": "doc", "content": content})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = CompileDocument(1, raw); err == nil {
			t.Fatal("over-node-count document accepted")
		}
	})
	t.Run("link count", func(t *testing.T) {
		content := make([]any, maxDocumentLinks+1)
		for index := range content {
			content[index] = map[string]any{"type": "text", "text": "x", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "/safe"}}}}
		}
		raw, err := json.Marshal(map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": content}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = CompileDocument(1, raw); err == nil {
			t.Fatal("over-link-count document accepted")
		}
	})
	t.Run("link length", func(t *testing.T) {
		if _, err := CompileDocument(1, json.RawMessage(linkDocument("/"+strings.Repeat("a", maxLinkLength), ``))); err == nil {
			t.Fatal("overlong link accepted")
		}
	})
	t.Run("derived HTML", func(t *testing.T) {
		text := strings.Repeat("&", maxHTMLBytes/5+1)
		raw, err := json.Marshal(map[string]any{
			"type": "doc",
			"content": []any{map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": text}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = CompileDocument(1, raw); err == nil {
			t.Fatal("oversized derived HTML accepted")
		}
	})
}

func markedTextDocument(marks string) string {
	return `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[` + marks + `]}]}]}`
}

func linkDocument(href, extraAttrs string) string {
	encoded, _ := json.Marshal(href)
	return markedTextDocument(`{"type":"link","attrs":{"href":` + string(encoded) + extraAttrs + `}}`)
}

func TestBodyValidateRejectsNonCanonicalDerivedFields(t *testing.T) {
	body, err := CompileDocument(1, json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"contenuto valido"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body.HTML = "<script>alert(1)</script>"
	if err := body.Validate(); err == nil {
		t.Fatal("expected noncanonical HTML rejection")
	}
}
