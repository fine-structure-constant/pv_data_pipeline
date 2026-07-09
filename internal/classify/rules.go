package classify

import (
	"regexp"
	"strings"
)

type RuleResult struct {
	Tags         []TagEvidence
	Relevant     bool
	MAPbI3Only   bool
	Confidence   float64
	Compositions []string
}

type TagEvidence struct {
	Code       string
	Confidence float64
	Evidence   string
}

func ApplyRules(title, abstract string, keywords []string) RuleResult {
	text := strings.ToLower(strings.Join(append([]string{title, abstract}, keywords...), " "))
	added := map[string]TagEvidence{}
	add := func(code string, conf float64, evidence string) {
		if old, ok := added[code]; !ok || conf > old.Confidence {
			added[code] = TagEvidence{Code: code, Confidence: conf, Evidence: evidence}
		}
	}
	contains := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(text, strings.ToLower(p)) {
				return true
			}
		}
		return false
	}
	if contains("perovskite solar cell", "solar cells", "photovoltaic") && contains("perovskite") {
		add("PB_BASED", 0.55, "mentions perovskite solar cell context")
	}
	if contains("formamidinium", "fapbi3", "fa pb i3", "fa-rich", "fa rich") {
		add("FA_RICH", 0.82, "mentions FA/formamidinium perovskite")
		add("FA_PB_I3", 0.78, "mentions FAPbI3 or FA Pb I3")
		add("NOT_MA_PB_I3", 0.75, "non-MAPbI3 family detected")
	}
	if contains("cspbi2br", "cs pb i2 br", "cesium lead iodide bromide") {
		add("CS_PB_I2_BR", 0.85, "mentions CsPbI2Br family")
		add("CS_CONTAINING", 0.85, "mentions cesium-containing perovskite")
		add("I_BR_MIXED", 0.78, "iodide/bromide mixed family")
		add("MIXED_HALIDE", 0.78, "mixed halide family")
		add("WIDE_BANDGAP", 0.65, "CsPbI2Br is commonly wide-bandgap")
		add("NOT_MA_PB_I3", 0.8, "non-MAPbI3 family detected")
	}
	if contains("fa0.85ma0.15", "fa ma", "mixed cation", "mixed-cation") {
		add("FA_MA_PB_I_BR", 0.8, "mentions FA/MA mixed cation")
		add("MIXED_CATION", 0.82, "mentions mixed cation")
		add("NOT_MA_PB_I3", 0.75, "non-MAPbI3 family detected")
	}
	if contains("mixed halide", "mixed-halide", "iodide bromide", "i/br", "i-br") {
		add("MIXED_HALIDE", 0.8, "mentions mixed halide")
		add("I_BR_MIXED", 0.78, "mentions iodide/bromide")
	}
	if contains("wide bandgap", "wide-bandgap") {
		add("WIDE_BANDGAP", 0.78, "mentions wide bandgap")
	}
	if contains("tin perovskite", "sn perovskite", "fasni3", "fa sn i3", "pb-sn", "pb sn") {
		add("SN_BASED", 0.82, "mentions Sn-based perovskite")
		if contains("fasni3", "fa sn i3") {
			add("FA_SN_I3", 0.86, "mentions FASnI3")
		}
		add("NOT_MA_PB_I3", 0.82, "non-MAPbI3 family detected")
	}
	if contains("2d perovskite", "two-dimensional perovskite", "low-dimensional") {
		add("LOW_DIMENSIONAL", 0.72, "mentions low-dimensional perovskite")
	}
	if contains("3d perovskite", "three-dimensional perovskite") {
		add("THREE_D", 0.7, "mentions 3D perovskite")
	}

	mapbi3 := contains("mapbi3", "ma pbi3", "methylammonium lead iodide")
	hasNonMA := false
	for code := range added {
		if code == "NOT_MA_PB_I3" || code == "FA_RICH" || code == "CS_CONTAINING" || code == "SN_BASED" || code == "MIXED_CATION" || code == "MIXED_HALIDE" {
			hasNonMA = true
		}
	}
	if mapbi3 && !hasNonMA {
		add("MAPBI3_BASELINE", 0.72, "mentions MAPbI3 without clear non-MAPbI3 family")
	}
	if !contains("perovskite") || (!contains("solar cell", "photovoltaic", "pce", "power conversion efficiency") && contains("oxide perovskite")) {
		add("IRRELEVANT", 0.7, "not clearly halide perovskite solar cell literature")
	}

	tags := make([]TagEvidence, 0, len(added))
	for _, t := range added {
		tags = append(tags, t)
	}
	return RuleResult{
		Tags: tags, Relevant: !hasTag(tags, "IRRELEVANT") && (contains("perovskite") && contains("solar cell", "photovoltaic", "pce", "power conversion efficiency")),
		MAPbI3Only: mapbi3 && !hasNonMA, Confidence: confidence(tags), Compositions: ExtractFormulaHints(title + " " + abstract),
	}
}

var formulaRE = regexp.MustCompile(`(?i)\b(?:FA|MA|Cs|Rb|K|Pb|Sn)[A-Za-z0-9\.\(\)]*(?:Pb|Sn)[A-Za-z0-9\.\(\)]*(?:I|Br|Cl)[A-Za-z0-9\.\(\)]*\b`)

func ExtractFormulaHints(text string) []string {
	matches := formulaRE.FindAllString(text, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func hasTag(tags []TagEvidence, code string) bool {
	for _, t := range tags {
		if t.Code == code {
			return true
		}
	}
	return false
}

func confidence(tags []TagEvidence) float64 {
	best := 0.0
	for _, t := range tags {
		if t.Confidence > best {
			best = t.Confidence
		}
	}
	return best
}
