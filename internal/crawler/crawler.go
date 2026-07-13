package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"pvsk-pipeline/internal/models"
	"pvsk-pipeline/internal/sources"
	"pvsk-pipeline/internal/util"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Service struct {
	DB        *gorm.DB
	Source    sources.LiteratureSource
	RateLimit time.Duration
}

type Options struct {
	Query           string
	Limit           int
	Include         []string
	Exclude         []string
	CandidateFactor int
}

// Crawl keeps fetching candidate pages until Limit accepted records have been
// stored or the source is exhausted. Limit therefore means accepted results.
func (s Service) Crawl(ctx context.Context, opts Options) (*models.CrawlJob, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.CandidateFactor <= 0 {
		opts.CandidateFactor = 8
	}
	job := &models.CrawlJob{
		Query: describeQuery(opts), Source: s.Source.Name(), Status: "running", StartedAt: time.Now().UTC(),
	}
	if err := s.DB.Create(job).Error; err != nil {
		return nil, err
	}
	pageSize := opts.Limit * 2
	if pageSize < 50 {
		pageSize = 50
	}
	if pageSize > 250 {
		pageSize = 250
	}
	maxCandidates := opts.Limit * opts.CandidateFactor
	accepted := 0
	for offset := 0; offset < maxCandidates && accepted < opts.Limit; offset += pageSize {
		want := pageSize
		if remaining := maxCandidates - offset; want > remaining {
			want = remaining
		}
		candidates, err := s.Source.Search(ctx, sources.SearchOptions{Query: opts.Query, Limit: want, Offset: offset})
		if err != nil {
			s.finish(job, "failed", err.Error())
			return job, err
		}
		job.NumFound += len(candidates)
		for _, c := range candidates {
			if accepted >= opts.Limit {
				break
			}
			select {
			case <-ctx.Done():
				s.finish(job, "failed", ctx.Err().Error())
				return job, ctx.Err()
			default:
			}
			if ok, reason := matches(c, opts.Include, opts.Exclude); !ok {
				s.log(job.ID, "debug", c.DOI, "filtered: "+reason)
				continue
			}
			if s.RateLimit > 0 {
				time.Sleep(s.RateLimit)
			}
			action, err := s.upsertCandidate(c)
			if err != nil {
				job.NumFailed++
				s.log(job.ID, "error", c.DOI, err.Error())
				continue
			}
			if action == "inserted" {
				job.NumInserted++
			} else if action == "updated" {
				job.NumUpdated++
			}
			accepted++
		}
		if len(candidates) < want {
			break
		}
	}
	s.finish(job, "done", "")
	return job, s.DB.Save(job).Error
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)
var supplementaryDOI = regexp.MustCompile(`(?i)\.s\d+$`)

func normalized(s string) string { return nonWord.ReplaceAllString(strings.ToLower(s), "") }

func matches(c sources.PaperCandidate, includes, excludes []string) (bool, string) {
	if supplementaryDOI.MatchString(c.DOI) {
		return false, "supplementary-information DOI"
	}
	if c.Kind != "" && c.Kind != "journal-article" && c.Kind != "proceedings-article" && c.Kind != "posted-content" {
		return false, "unsupported record type " + c.Kind
	}
	if strings.TrimSpace(c.Title) == "" {
		return false, "missing title"
	}
	haystack := normalized(strings.Join(append([]string{c.Title, c.Abstract}, c.Keywords...), " "))
	for _, term := range excludes {
		if t := normalized(term); t != "" && strings.Contains(haystack, t) {
			return false, "excluded by " + term
		}
	}
	for _, term := range includes {
		if t := normalized(term); t != "" && !strings.Contains(haystack, t) {
			return false, "missing " + term
		}
	}
	return true, ""
}

func describeQuery(opts Options) string {
	b, _ := json.Marshal(map[string]any{"query": opts.Query, "include": opts.Include, "exclude": opts.Exclude, "limit": opts.Limit})
	return string(b)
}

func (s Service) upsertCandidate(c sources.PaperCandidate) (string, error) {
	doi := util.NormalizeDOI(c.DOI)
	titleHash := util.TitleHash(c.Title)
	authors, _ := json.Marshal(c.Authors)
	raw := c.RawMetadata
	if len(raw) == 0 {
		raw, _ = json.Marshal(c)
	}
	paper := models.Paper{
		DOI: doi, Title: c.Title, TitleHash: titleHash, Abstract: c.Abstract,
		Journal: c.Journal, Year: c.Year, PublishedDate: c.PublishedDate,
		Authors: datatypes.JSON(authors), URL: c.URL, SourceAPI: c.SourceAPI,
		RawMetadata: datatypes.JSON(raw), CrawlStatus: "crawled",
		DownloadStatus: "metadata_only", ExtractionStatus: "not_started",
	}

	var existing models.Paper
	var err error
	if doi != "" {
		err = s.DB.Where("doi = ?", doi).First(&existing).Error
	} else {
		err = s.DB.Where("title_hash = ?", titleHash).First(&existing).Error
	}
	if err == nil {
		existing.Title = paper.Title
		existing.Abstract = paper.Abstract
		existing.Journal = paper.Journal
		existing.Year = paper.Year
		existing.PublishedDate = paper.PublishedDate
		existing.Authors = paper.Authors
		existing.URL = paper.URL
		existing.SourceAPI = paper.SourceAPI
		existing.RawMetadata = paper.RawMetadata
		existing.CrawlStatus = "crawled"
		if err := s.DB.Save(&existing).Error; err != nil {
			return "", err
		}
		if c.PDFURL != "" || c.OpenAccessURL != "" {
			return "updated", s.upsertCandidateAsset(existing.ID, c)
		}
		return "updated", nil
	}
	if err != gorm.ErrRecordNotFound {
		return "", err
	}
	if err := s.DB.Create(&paper).Error; err != nil {
		return "", err
	}
	if c.PDFURL != "" || c.OpenAccessURL != "" {
		return "inserted", s.upsertCandidateAsset(paper.ID, c)
	}
	return "inserted", nil
}

func (s Service) upsertCandidateAsset(paperID string, c sources.PaperCandidate) error {
	sourceURL := c.PDFURL
	assetType := "pdf"
	if sourceURL == "" {
		sourceURL = c.OpenAccessURL
		assetType = "html"
	}
	var existing models.PaperAsset
	err := s.DB.Where("paper_id = ? AND source_url = ?", paperID, sourceURL).First(&existing).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	asset := models.PaperAsset{
		PaperID: paperID, AssetType: assetType, SourceURL: sourceURL,
		License: c.License, AccessType: "open_access",
	}
	return s.DB.Create(&asset).Error
}

func (s Service) finish(job *models.CrawlJob, status, errMsg string) {
	now := time.Now().UTC()
	job.Status = status
	job.FinishedAt = &now
	job.ErrorMessage = errMsg
	if saveErr := s.DB.Save(job).Error; saveErr != nil {
		log.Printf("crawl job save failed: %v", saveErr)
	}
}

func (s Service) log(jobID, level, doi, message string) {
	entry := models.CrawlLog{CrawlJobID: jobID, Level: level, DOI: doi, Message: message}
	if err := s.DB.Create(&entry).Error; err != nil {
		log.Printf("crawl log failed: %v", err)
	}
}

func Summary(job *models.CrawlJob) string {
	return fmt.Sprintf("job=%s status=%s found=%d inserted_or_updated=%d failed=%d", job.ID, job.Status, job.NumFound, job.NumInserted+job.NumUpdated, job.NumFailed)
}
