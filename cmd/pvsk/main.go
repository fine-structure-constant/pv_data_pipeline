package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"pvsk-pipeline/internal/classify"
	"pvsk-pipeline/internal/config"
	"pvsk-pipeline/internal/crawler"
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
	cfg := config.Load()
	switch os.Args[1] {
	case "migrate":
		mustMigrate(cfg)
	case "crawl":
		mustCrawl(cfg, os.Args[2:])
	case "classify":
		mustClassify(cfg, os.Args[2:])
	case "download":
		mustDownload(cfg, os.Args[2:])
	case "serve":
		mustServe(cfg, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  go run ./cmd/pvsk migrate
  go run ./cmd/pvsk crawl --query "FA Pb I3 perovskite solar cell" --limit 50
  go run ./cmd/pvsk classify --limit 100
  go run ./cmd/pvsk download --limit 100
  go run ./cmd/pvsk serve --addr ":8080"`)
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
	job, err := crawler.Service{DB: gdb, Source: source, RateLimit: cfg.CrawlRateLimit}.Crawl(ctx, *query, *limit)
	if err != nil {
		log.Fatalf("crawl failed: %v", err)
	}
	log.Printf("crawl complete: %s", crawler.Summary(job))
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
		HTTP: &http.Client{Timeout: cfg.LLMTimeout}, UserAgent: cfg.HTTPUserAgent,
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
