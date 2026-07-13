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
	Kind          string
}

// SearchOptions describes discovery separately from local acceptance filtering.
// Sources should return up to Limit candidates; the crawler decides which ones
// satisfy include/exclude constraints.
type SearchOptions struct {
	Query  string
	Limit  int
	Offset int
}

type LiteratureSource interface {
	Search(ctx context.Context, opts SearchOptions) ([]PaperCandidate, error)
	Name() string
}
