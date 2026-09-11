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
