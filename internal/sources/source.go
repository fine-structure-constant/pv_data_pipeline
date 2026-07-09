package sources

import (
	"context"
	"time"
)

type PaperCandidate struct {
	DOI           string
	Title         string
	Abstract      string
	Journal       string
	Year          int
	PublishedDate *time.Time
	Authors       []string
	URL           string
	SourceAPI     string
	RawMetadata   []byte
	Keywords      []string
	OpenAccessURL string
	PDFURL        string
	License       string
}

type LiteratureSource interface {
	Search(ctx context.Context, query string, limit int) ([]PaperCandidate, error)
	Name() string
}
