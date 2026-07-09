package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pvsk-pipeline/internal/models"
	"pvsk-pipeline/internal/storage"

	"gorm.io/gorm"
)

type Service struct {
	DB        *gorm.DB
	Storage   storage.Manager
	Client    *http.Client
	UserAgent string
	RateLimit time.Duration
	MaxBytes  int64
}

func (s Service) DownloadPending(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 20
	}
	var papers []models.Paper
	if err := s.DB.Preload("Assets", "access_type = ? AND local_path = ''", "open_access").
		Where("download_status IN ?", []string{"metadata_only", "download_failed", "not_available", ""}).
		Order("created_at asc").Limit(limit).Find(&papers).Error; err != nil {
		return err
	}
	for _, p := range papers {
		if err := s.downloadPaper(ctx, p); err != nil {
			log.Printf("download paper=%s doi=%s failed: %v", p.ID, p.DOI, err)
		}
		if s.RateLimit > 0 {
			time.Sleep(s.RateLimit)
		}
	}
	return nil
}

func (s Service) downloadPaper(ctx context.Context, p models.Paper) error {
	dir, err := s.Storage.EnsurePaperDir(p.ID)
	if err != nil {
		return err
	}
	p.LocalDir = dir
	if _, err := s.Storage.WriteMetadata(p); err != nil {
		return err
	}
	downloaded := 0
	for _, asset := range p.Assets {
		if asset.SourceURL == "" {
			continue
		}
		if err := s.downloadAsset(ctx, p.ID, dir, &asset); err != nil {
			asset.ErrorMessage = err.Error()
			_ = s.DB.Save(&asset).Error
			log.Printf("asset download failed paper=%s url=%s err=%v", p.ID, asset.SourceURL, err)
			continue
		}
		downloaded++
	}
	status := "not_available"
	if downloaded > 0 {
		status = "open_access_downloaded"
	} else if len(p.Assets) > 0 {
		status = "download_failed"
	}
	return s.DB.Model(&models.Paper{}).Where("id = ?", p.ID).Updates(map[string]any{
		"local_dir": dir, "download_status": status,
	}).Error
}

func (s Service) downloadAsset(ctx context.Context, paperID, dir string, asset *models.PaperAsset) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.SourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/pdf,text/html,application/xml,*/*;q=0.8")
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	ext := extensionFor(asset.AssetType, contentType, asset.SourceURL)
	name := "asset" + ext
	switch asset.AssetType {
	case "pdf":
		name = "paper.pdf"
	case "html":
		name = "fulltext.html"
	case "xml":
		name = "fulltext.xml"
	}
	localPath := filepath.Join(dir, name)
	tmp := localPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	max := s.MaxBytes
	if max <= 0 {
		max = 80 * 1024 * 1024
	}
	_, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, max+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, localPath); err != nil {
		return err
	}
	now := time.Now().UTC()
	asset.LocalPath = localPath
	asset.SHA256 = hex.EncodeToString(h.Sum(nil))
	asset.MIMEType = contentType
	asset.DownloadedAt = &now
	asset.ErrorMessage = ""
	return s.DB.Save(asset).Error
}

func extensionFor(assetType, contentType, sourceURL string) string {
	if assetType == "pdf" {
		return ".pdf"
	}
	if assetType == "html" {
		return ".html"
	}
	if assetType == "xml" {
		return ".xml"
	}
	if exts, err := mime.ExtensionsByType(strings.Split(contentType, ";")[0]); err == nil && len(exts) > 0 {
		return exts[0]
	}
	lower := strings.ToLower(sourceURL)
	for _, ext := range []string{".pdf", ".html", ".xml", ".json"} {
		if strings.Contains(lower, ext) {
			return ext
		}
	}
	return ".bin"
}
