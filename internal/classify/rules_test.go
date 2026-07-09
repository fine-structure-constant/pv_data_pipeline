package classify

import "testing"

func TestApplyRulesMixedFA(t *testing.T) {
	r := ApplyRules("FA0.85MA0.15 Pb I/Br perovskite solar cell", "wide-bandgap mixed cation device", nil)
	want := []string{"FA_MA_PB_I_BR", "MIXED_CATION", "MIXED_HALIDE", "WIDE_BANDGAP", "NOT_MA_PB_I3"}
	for _, code := range want {
		if !hasTag(r.Tags, code) {
			t.Fatalf("missing tag %s in %#v", code, r.Tags)
		}
	}
	if r.MAPbI3Only {
		t.Fatal("mixed FA/MA paper should not be MAPbI3-only")
	}
}

func TestApplyRulesMAPbI3Baseline(t *testing.T) {
	r := ApplyRules("MAPbI3 perovskite solar cell", "methylammonium lead iodide baseline", nil)
	if !hasTag(r.Tags, "MAPBI3_BASELINE") || !r.MAPbI3Only {
		t.Fatalf("expected MAPbI3 baseline, got %#v", r)
	}
}
