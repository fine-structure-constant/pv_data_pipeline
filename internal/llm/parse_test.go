package llm

import "testing"

func TestParseClassification(t *testing.T) {
	raw := `{"is_relevant_perovskite_solar_cell":true,"is_halide_perovskite":true,"is_solar_cell":true,"is_mapbi3_only":false,"families":["FA_PB_I3","WIDE_BANDGAP"],"confidence":0.87}`
	got, parsed, err := ParseClassification(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsRelevantPerovskiteSolarCell || got.IsMAPbI3Only || got.Confidence != 0.87 || len(parsed) == 0 {
		t.Fatalf("unexpected parse result: %#v parsed=%s", got, parsed)
	}
}

func TestParseClassificationRejectsInvalidJSON(t *testing.T) {
	if _, _, err := ParseClassification("not json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
