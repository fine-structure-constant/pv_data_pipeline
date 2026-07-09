package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

func (s Service) Crawl(ctx context.Context, query string, limit int) (*models.CrawlJob, error) {
	job := &models.CrawlJob{
		Query: query, Source: s.Source.Name(), Status: "running", StartedAt: time.Now().UTC(),
	}
	if err := s.DB.Create(job).Error; err != nil {
		return nil, err
	}
	candidates, err := s.Source.Search(ctx, query, limit)
	if err != nil {
		s.finish(job, "failed", err.Error())
		return job, err
	}
	job.NumFound = len(candidates)
	for _, c := range candidates {
		select {
		case <-ctx.Done():
			s.finish(job, "failed", ctx.Err().Error())
			return job, ctx.Err()
		default:
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
	}
	s.finish(job, "done", "")
	return job, s.DB.Save(job).Error
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
