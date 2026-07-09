package llm

import (
	"encoding/json"
	"errors"
	"strings"
)

type ClassificationResult struct {
	IsRelevantPerovskiteSolarCell bool                   `json:"is_relevant_perovskite_solar_cell"`
	IsHalidePerovskite            bool                   `json:"is_halide_perovskite"`
	IsSolarCell                   bool                   `json:"is_solar_cell"`
	IsMAPbI3Only                  bool                   `json:"is_mapbi3_only"`
	Priority                      string                 `json:"priority"`
	Families                      []string               `json:"families"`
	DetectedCompositions          []map[string]any       `json:"detected_compositions"`
	Evidence                      []string               `json:"evidence"`
	Confidence                    float64                `json:"confidence"`
	Extra                         map[string]interface{} `json:"-"`
}

func ParseClassification(raw string) (ClassificationResult, []byte, error) {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return ClassificationResult{}, nil, errors.New("empty LLM response")
	}
	var result ClassificationResult
	if err := json.Unmarshal([]byte(clean), &result); err != nil {
		return ClassificationResult{}, nil, err
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(clean), &generic); err != nil {
		return ClassificationResult{}, nil, err
	}
	parsed, _ := json.Marshal(generic)
	return result, parsed, nil
}
