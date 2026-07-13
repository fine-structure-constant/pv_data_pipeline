package crawler

import (
	"testing"

	"pvsk-pipeline/internal/sources"
)

func TestMatchesFuzzyCompositionAndExclusion(t *testing.T) {
	c := sources.PaperCandidate{Title: "Formamidinium FA PbI3 perovskite solar cells", Abstract: "stable device"}
	if ok, reason := matches(c, []string{"FAPbI3"}, []string{"MAPbI3"}); !ok {
		t.Fatalf("expected normalized fuzzy match: %s", reason)
	}
	c.Abstract += " compared with MAPbI3"
	if ok, _ := matches(c, []string{"FAPbI3"}, []string{"MAPbI3"}); ok {
		t.Fatal("expected exclusion to take precedence")
	}
}

func TestMatchesRejectsSupplementaryDOI(t *testing.T) {
	c := sources.PaperCandidate{DOI: "10.1021/example.s001", Title: "FAPbI3 perovskite"}
	if ok, _ := matches(c, nil, nil); ok {
		t.Fatal("expected SI DOI rejection")
	}
}
