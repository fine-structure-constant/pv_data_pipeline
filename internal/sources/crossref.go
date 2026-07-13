package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pvsk-pipeline/internal/util"
)

type CrossrefSource struct {
	Client    *http.Client
	UserAgent string
	BaseURL   string
}

func NewCrossref(client *http.Client, userAgent string) *CrossrefSource {
	return &CrossrefSource{
		Client: client, UserAgent: userAgent, BaseURL: "https://api.crossref.org/works",
	}
}

func (s *CrossrefSource) Name() string { return "crossref" }

func (s *CrossrefSource) Search(ctx context.Context, opts SearchOptions) ([]PaperCandidate, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query.bibliographic", opts.Query)
	q.Set("rows", fmt.Sprintf("%d", opts.Limit))
	q.Set("offset", fmt.Sprintf("%d", opts.Offset))
	q.Set("filter", "type:journal-article")
	q.Set("select", "DOI,title,abstract,container-title,published-print,published-online,published,author,URL,link,license,subject,issued")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("crossref search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var cr crossrefResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 20_000_000)).Decode(&cr); err != nil {
		return nil, err
	}
	out := make([]PaperCandidate, 0, len(cr.Message.Items))
	for _, item := range cr.Message.Items {
		raw, _ := json.Marshal(item)
		pc := PaperCandidate{
			DOI:         util.NormalizeDOI(item.DOI),
			Title:       first(item.Title),
			Abstract:    cleanAbstract(item.Abstract),
			Journal:     first(item.ContainerTitle),
			Year:        yearFromDateParts(item.Issued.DateParts),
			URL:         item.URL,
			SourceAPI:   s.Name(),
			RawMetadata: raw,
			Keywords:    item.Subject,
			License:     firstLicense(item.License),
			Kind:        item.Type,
		}
		if t := dateFromParts(item.PublishedOnline.DateParts); t != nil {
			pc.PublishedDate = t
			pc.Year = t.Year()
		} else if t := dateFromParts(item.PublishedPrint.DateParts); t != nil {
			pc.PublishedDate = t
			pc.Year = t.Year()
		} else if t := dateFromParts(item.Published.DateParts); t != nil {
			pc.PublishedDate = t
			pc.Year = t.Year()
		}
		for _, a := range item.Author {
			name := strings.TrimSpace(strings.Join([]string{a.Given, a.Family}, " "))
			if name != "" {
				pc.Authors = append(pc.Authors, name)
			}
		}
		for _, l := range item.Link {
			ct := strings.ToLower(l.ContentType)
			if strings.Contains(ct, "pdf") || strings.HasSuffix(strings.ToLower(l.URL), ".pdf") {
				pc.PDFURL = l.URL
				pc.OpenAccessURL = l.URL
				break
			}
			if strings.Contains(ct, "html") && pc.OpenAccessURL == "" {
				pc.OpenAccessURL = l.URL
			}
		}
		out = append(out, pc)
	}
	return out, nil
}

type crossrefResponse struct {
	Message struct {
		Items []crossrefItem `json:"items"`
	} `json:"message"`
}

type crossrefItem struct {
	DOI             string            `json:"DOI"`
	Title           []string          `json:"title"`
	Abstract        string            `json:"abstract"`
	ContainerTitle  []string          `json:"container-title"`
	PublishedPrint  crossrefDate      `json:"published-print"`
	PublishedOnline crossrefDate      `json:"published-online"`
	Published       crossrefDate      `json:"published"`
	Issued          crossrefDate      `json:"issued"`
	Author          []crossrefAuthor  `json:"author"`
	URL             string            `json:"URL"`
	Link            []crossrefLink    `json:"link"`
	License         []crossrefLicense `json:"license"`
	Subject         []string          `json:"subject"`
	Type            string            `json:"type"`
}

type crossrefDate struct {
	DateParts [][]int `json:"date-parts"`
}

type crossrefAuthor struct {
	Given  string `json:"given"`
	Family string `json:"family"`
}

type crossrefLink struct {
	URL         string `json:"URL"`
	ContentType string `json:"content-type"`
}

type crossrefLicense struct {
	URL string `json:"URL"`
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return strings.TrimSpace(v[0])
}

func firstLicense(v []crossrefLicense) string {
	if len(v) == 0 {
		return ""
	}
	return v[0].URL
}

func cleanAbstract(s string) string {
	s = html.UnescapeString(s)
	replacer := strings.NewReplacer("<jats:p>", "", "</jats:p>", "", "<p>", "", "</p>", "")
	return strings.TrimSpace(replacer.Replace(s))
}

func yearFromDateParts(parts [][]int) int {
	if len(parts) == 0 || len(parts[0]) == 0 {
		return 0
	}
	return parts[0][0]
}

func dateFromParts(parts [][]int) *time.Time {
	if len(parts) == 0 || len(parts[0]) == 0 {
		return nil
	}
	p := parts[0]
	month, day := 1, 1
	if len(p) > 1 && p[1] >= 1 && p[1] <= 12 {
		month = p[1]
	}
	if len(p) > 2 && p[2] >= 1 && p[2] <= 31 {
		day = p[2]
	}
	t := time.Date(p[0], time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return &t
}
