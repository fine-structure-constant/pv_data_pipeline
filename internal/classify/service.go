package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"pvsk-pipeline/internal/llm"
	"pvsk-pipeline/internal/models"
	"pvsk-pipeline/internal/util"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	DB            *gorm.DB
	LLM           llm.Client
	PromptPath    string
	PromptVersion string
	LLMTimeout    time.Duration
}

func (s Service) ClassifyPending(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 20
	}
	var papers []models.Paper
	if err := s.DB.Order("created_at asc").Limit(limit).Find(&papers).Error; err != nil {
		return err
	}
	prompt, err := os.ReadFile(s.PromptPath)
	if err != nil {
		return fmt.Errorf("read prompt: %w", err)
	}
	for _, paper := range papers {
		if err := s.classifyOne(ctx, paper, string(prompt)); err != nil {
			log.Printf("classify paper=%s doi=%s failed: %v", paper.ID, paper.DOI, err)
		}
	}
	return nil
}

func (s Service) classifyOne(ctx context.Context, paper models.Paper, prompt string) error {
	var keywords []string
	rule := ApplyRules(paper.Title, paper.Abstract, keywords)
	if err := s.assignRuleTags(paper.ID, rule); err != nil {
		return err
	}
	input := buildInput(paper, rule.Compositions)
	inputHash := util.SHA256Hex(input)
	rec := models.LLMClassification{
		PaperID: paper.ID, PromptVersion: s.PromptVersion, InputTextHash: inputHash,
		IsRelevant: rule.Relevant, IsMAPbI3Only: rule.MAPbI3Only, Confidence: rule.Confidence,
	}
	if !s.LLM.Enabled() {
		rec.ModelName = "rule_fallback"
		rec.ErrorMessage = "LLM skipped because provider or API key is empty"
		if err := s.DB.Create(&rec).Error; err != nil {
			return err
		}
		return s.updateExtractionStatus(paper.ID, "done")
	}
	timeout := s.LLMTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	llmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := s.LLM.Classify(llmCtx, prompt, input)
	rec.ModelName = s.LLM.Model
	rec.RawResponse = raw
	if err != nil {
		rec.ErrorMessage = err.Error()
		_ = s.DB.Create(&rec).Error
		_ = s.updateExtractionStatus(paper.ID, "failed")
		return err
	}
	parsed, parsedJSON, err := llm.ParseClassification(raw)
	if err != nil {
		rec.ErrorMessage = "parse LLM JSON: " + err.Error()
		_ = s.DB.Create(&rec).Error
		_ = s.updateExtractionStatus(paper.ID, "failed")
		return err
	}
	rec.ParsedJSON = datatypes.JSON(parsedJSON)
	rec.IsRelevant = parsed.IsRelevantPerovskiteSolarCell
	rec.IsMAPbI3Only = parsed.IsMAPbI3Only
	rec.Confidence = parsed.Confidence
	if err := s.DB.Create(&rec).Error; err != nil {
		return err
	}
	if err := s.assignLLMTags(paper.ID, parsed); err != nil {
		return err
	}
	return s.updateExtractionStatus(paper.ID, "done")
}

func (s Service) assignRuleTags(paperID string, rule RuleResult) error {
	for _, t := range rule.Tags {
		if err := s.assignTag(paperID, t.Code, t.Confidence, "rule", t.Evidence); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) assignLLMTags(paperID string, parsed llm.ClassificationResult) error {
	evidence := strings.Join(parsed.Evidence, " | ")
	for _, code := range parsed.Families {
		if err := s.assignTag(paperID, code, parsed.Confidence, "llm", evidence); err != nil {
			return err
		}
	}
	if parsed.IsMAPbI3Only {
		return s.assignTag(paperID, "MAPBI3_BASELINE", parsed.Confidence, "llm", evidence)
	}
	if parsed.IsRelevantPerovskiteSolarCell && !parsed.IsMAPbI3Only {
		return s.assignTag(paperID, "NOT_MA_PB_I3", parsed.Confidence, "llm", evidence)
	}
	if !parsed.IsRelevantPerovskiteSolarCell {
		return s.assignTag(paperID, "IRRELEVANT", parsed.Confidence, "llm", evidence)
	}
	return nil
}

func (s Service) assignTag(paperID, code string, confidence float64, assignedBy, evidence string) error {
	var cls models.MaterialClass
	if err := s.DB.Where("code = ?", code).First(&cls).Error; err != nil {
		return err
	}
	link := models.PaperMaterialClass{
		PaperID: paperID, MaterialClassID: cls.ID, Confidence: confidence, AssignedBy: assignedBy, EvidenceText: evidence,
	}
	return s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "paper_id"}, {Name: "material_class_id"}, {Name: "assigned_by"}},
		DoUpdates: clause.AssignmentColumns([]string{"confidence", "evidence_text"}),
	}).Create(&link).Error
}

func (s Service) updateExtractionStatus(paperID, status string) error {
	return s.DB.Model(&models.Paper{}).Where("id = ?", paperID).Update("extraction_status", status).Error
}

func buildInput(p models.Paper, formulas []string) string {
	var authors []string
	_ = json.Unmarshal(p.Authors, &authors)
	return fmt.Sprintf("title: %s\nabstract: %s\nkeywords: \ndoi: %s\njournal: %s\nyear: %d\nauthors: %s\npossible_formula_strings: %s\n",
		p.Title, p.Abstract, p.DOI, p.Journal, p.Year, strings.Join(authors, "; "), strings.Join(formulas, "; "))
}
