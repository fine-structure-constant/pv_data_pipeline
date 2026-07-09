package util

import "testing"

func TestNormalizeDOI(t *testing.T) {
	tests := map[string]string{
		" HTTPS://DOI.ORG/10.1000/ABC ":   "10.1000/abc",
		"http://dx.doi.org/10.1038/NMAT ": "10.1038/nmat",
		"doi:10.1016/J.SOLMAT.2020.1":     "10.1016/j.solmat.2020.1",
		"10.1021/acsenergylett.1c00001":   "10.1021/acsenergylett.1c00001",
	}
	for in, want := range tests {
		if got := NormalizeDOI(in); got != want {
			t.Fatalf("NormalizeDOI(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTitleHashWhitespaceInsensitive(t *testing.T) {
	a := TitleHash("Wide bandgap   perovskite")
	b := TitleHash(" wide bandgap perovskite ")
	if a != b {
		t.Fatalf("title hashes differ: %s %s", a, b)
	}
}
