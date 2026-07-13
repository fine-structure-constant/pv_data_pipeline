package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"pvsk-pipeline/internal/classify"
	"pvsk-pipeline/internal/config"
	"pvsk-pipeline/internal/crawler"
	"pvsk-pipeline/internal/data2"
	"pvsk-pipeline/internal/db"
	"pvsk-pipeline/internal/download"
	"pvsk-pipeline/internal/llm"
	"pvsk-pipeline/internal/server"
	"pvsk-pipeline/internal/sources"
	"pvsk-pipeline/internal/storage"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	configPath, command, args := parseGlobalArgs(os.Args[1:])
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config %s: %v", configPath, err)
	}
	switch command {
	case "migrate":
		mustMigrate(cfg)
	case "crawl":
		mustCrawl(cfg, args)
	case "classify":
		mustClassify(cfg, args)
	case "download":
		mustDownload(cfg, args)
	case "merge-data2":
		mustMergeData2(cfg, args)
	case "serve":
		mustServe(cfg, args)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  go run ./cmd/pvsk [--config config.yaml] migrate
  go run ./cmd/pvsk [--config config.yaml] crawl --query "perovskite solar cell" --include FAPbI3 --exclude MAPbI3 --limit 50 --extract
  go run ./cmd/pvsk [--config config.yaml] classify --limit 100
  go run ./cmd/pvsk [--config config.yaml] download --limit 100
  go run ./cmd/pvsk [--config config.yaml] merge-data2 --file ../data2_progress.xlsx
  go run ./cmd/pvsk [--config config.yaml] serve --addr ":8080"`)
}

func parseGlobalArgs(args []string) (string, string, []string) {
	configPath := "config.yaml"
	for len(args) > 0 {
		switch {
		case args[0] == "--config" && len(args) > 1:
			configPath = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--config="):
			configPath = strings.TrimPrefix(args[0], "--config=")
			args = args[1:]
		default:
			return configPath, args[0], args[1:]
		}
	}
	return configPath, "", nil
}

func mustMigrate(cfg config.Config) {
	gdb, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("migration complete")
}

func mustCrawl(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	query := fs.String("query", "", "search query")
	limit := fs.Int("limit", 50, "max results")
	include := fs.String("include", "", "comma-separated required fuzzy terms")
	exclude := fs.String("exclude", "", "comma-separated rejected fuzzy terms")
	candidateFactor := fs.Int("candidate-factor", 8, "maximum candidates examined per accepted result")
	extract := fs.Bool("extract", false, "classify and extract accepted pending papers after crawling")
	promptPath := fs.String("prompt", "prompts/classify_paper.md", "extraction prompt")
	sourceName := fs.String("source", "crossref", "literature source")
	_ = fs.Parse(args)
	if *query == "" {
		log.Fatal("--query is required")
	}
	gdb, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	httpClient := &http.Client{Timeout: cfg.RequestTimeout}
	var source sources.LiteratureSource
	switch *sourceName {
	case "crossref":
		source = sources.NewCrossref(httpClient, cfg.HTTPUserAgent)
	default:
		log.Fatalf("unsupported source %q; TODO adapters: openalex, semantic_scholar, unpaywall, local import", *sourceName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout+time.Duration(*limit)*cfg.CrawlRateLimit+2*time.Minute)
	defer cancel()
	job, err := crawler.Service{DB: gdb, Source: source, RateLimit: cfg.CrawlRateLimit}.Crawl(ctx, crawler.Options{
		Query: *query, Limit: *limit, Include: csvTerms(*include), Exclude: csvTerms(*exclude), CandidateFactor: *candidateFactor,
	})
	if err != nil {
		log.Fatalf("crawl failed: %v", err)
	}
	log.Printf("crawl complete: %s", crawler.Summary(job))
	if *extract {
		client := llm.Client{Provider: cfg.LLMProvider, BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel,
			HTTP: &http.Client{Timeout: cfg.LLMTimeout}, UserAgent: cfg.HTTPUserAgent, EnableWebSearch: cfg.LLMWebSearch}
		svc := classify.Service{DB: gdb, LLM: client, PromptPath: *promptPath, PromptVersion: "classify_paper_v2", LLMTimeout: cfg.LLMTimeout}
		if err := svc.ClassifyPending(context.Background(), *limit); err != nil {
			log.Fatalf("extract failed: %v", err)
		}
		log.Printf("extraction complete")
	}
}

func csvTerms(raw string) []string {
	var out []string
	for _, term := range strings.Split(raw, ",") {
		if term = strings.TrimSpace(term); term != "" {
			out = append(out, term)
		}
	}
	return out
}

func mustClassify(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("classify", flag.ExitOnError)
	limit := fs.Int("limit", 100, "max papers")
	promptPath := fs.String("prompt", "prompts/classify_paper.md", "classification prompt")
	_ = fs.Parse(args)
	gdb, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	client := llm.Client{
		Provider: cfg.LLMProvider, BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel,
		HTTP: &http.Client{Timeout: cfg.LLMTimeout}, UserAgent: cfg.HTTPUserAgent, EnableWebSearch: cfg.LLMWebSearch,
	}
	if !client.Enabled() {
		log.Printf("LLM disabled; using rule-based fallback only")
	}
	svc := classify.Service{DB: gdb, LLM: client, PromptPath: *promptPath, PromptVersion: "classify_paper_v1", LLMTimeout: cfg.LLMTimeout}
	if err := svc.ClassifyPending(context.Background(), *limit); err != nil {
		log.Fatalf("classify failed: %v", err)
	}
	log.Printf("classification complete")
}

func mustDownload(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	limit := fs.Int("limit", 100, "max papers")
	_ = fs.Parse(args)
	gdb, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	svc := download.Service{
		DB: gdb, Storage: storage.New(cfg.StorageRoot), Client: &http.Client{Timeout: cfg.RequestTimeout},
		UserAgent: cfg.HTTPUserAgent, RateLimit: cfg.CrawlRateLimit, MaxBytes: cfg.DownloadMaxBytes,
	}
	if err := svc.DownloadPending(ctx, *limit); err != nil {
		log.Fatalf("download failed: %v", err)
	}
	log.Printf("download complete")
}

func mustMergeData2(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("merge-data2", flag.ExitOnError)
	filePath := fs.String("file", "", "data2 xlsx/csv file")
	_ = fs.Parse(args)
	if *filePath == "" {
		log.Fatal("--file is required")
	}
	gdb, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	summary, err := data2.ImportFile(gdb, *filePath)
	if err != nil {
		log.Fatalf("merge data2: %v", err)
	}
	log.Printf("merge data2 complete: rows=%d skipped=%d papers inserted=%d matched=%d materials inserted=%d matched=%d devices inserted=%d matched=%d measurements inserted=%d updated=%d",
		summary.Rows, summary.Skipped, summary.PapersInserted, summary.PapersMatched, summary.MaterialsInserted, summary.MaterialsMatched,
		summary.DevicesInserted, summary.DevicesMatched, summary.MeasurementsInserted, summary.MeasurementsUpdated)
}

func mustServe(cfg config.Config, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	_ = fs.Parse(args)
	gdb, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	log.Printf("serving on %s", *addr)
	if err := http.ListenAndServe(*addr, server.Server{DB: gdb}.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
