package public

import "testing"

func TestETagMatchesUsesWeakComparison(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		header  string
		etag    string
		matches bool
	}{
		{name: "strong strong", header: `"same"`, etag: `"same"`, matches: true},
		{name: "weak request strong current", header: `W/"same"`, etag: `"same"`, matches: true},
		{name: "strong request weak current", header: `"same"`, etag: `W/"same"`, matches: true},
		{name: "list strong request weak current", header: `W/"other", "same"`, etag: `W/"same"`, matches: true},
		{name: "wildcard", header: `*`, etag: `W/"same"`, matches: true},
		{name: "malformed wildcard", header: `*invalid`, etag: `W/"same"`, matches: false},
		{name: "different", header: `W/"other"`, etag: `W/"same"`, matches: false},
		{name: "malformed", header: `same`, etag: `W/"same"`, matches: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := etagMatches(testCase.header, testCase.etag); got != testCase.matches {
				t.Fatalf("etagMatches(%q, %q) = %t, want %t", testCase.header, testCase.etag, got, testCase.matches)
			}
		})
	}
}
