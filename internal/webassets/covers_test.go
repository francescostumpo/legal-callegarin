package webassets

import (
	"regexp"
	"testing"
)

func TestEmbeddedCoverCatalogHasTwelveUniqueCompleteEntries(t *testing.T) {
	covers, err := CoverCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(covers) != 12 {
		t.Fatalf("covers = %d", len(covers))
	}
	seen := map[string]bool{}
	for _, cover := range covers {
		if cover.ID == "" || cover.Alt == "" || cover.CardAVIF == "" || cover.CardWebP == "" || cover.LandscapeAVIF == "" || cover.LandscapeWebP == "" || seen[cover.ID] {
			t.Fatalf("bad cover: %#v", cover)
		}
		for _, value := range []string{cover.CardAVIF, cover.CardWebP, cover.LandscapeAVIF, cover.LandscapeWebP} {
			if !regexp.MustCompile(`-[0-9a-f]{12}\.(?:avif|webp)$`).MatchString(value) {
				t.Fatalf("cover URL is not fingerprinted: %q", value)
			}
		}
		seen[cover.ID] = true
		if !ValidCoverID(cover.ID) {
			t.Fatalf("ValidCoverID(%q) = false", cover.ID)
		}
	}
	if !ValidCoverID("") || ValidCoverID("not-in-manifest") {
		t.Fatal("optional/unknown cover validation mismatch")
	}
}
