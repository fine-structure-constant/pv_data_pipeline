package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseDSN      string
	StorageRoot      string
	LLMProvider      string
	LLMBaseURL       string
	LLMAPIKey        string
	LLMModel         string
	HTTPUserAgent    string
	CrawlRateLimit   time.Duration
	RequestTimeout   time.Duration
	LLMTimeout       time.Duration
	DownloadMaxBytes int64
}

func Load() Config {
	loadDotEnv(".env")
	return Config{
		DatabaseDSN:      getenv("DATABASE_DSN", "postgres://pvsk_app:password@127.0.0.1:5432/pvsk_db?sslmode=disable"),
		StorageRoot:      getenv("PVSK_STORAGE_ROOT", "/home/rocky/HDDdata/perovskite_papers"),
		LLMProvider:      os.Getenv("LLM_PROVIDER"),
		LLMBaseURL:       getenv("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:        os.Getenv("LLM_API_KEY"),
		LLMModel:         getenv("LLM_MODEL", "gpt-5-mini"),
		HTTPUserAgent:    getenv("HTTP_USER_AGENT", "PvskCrawler/0.1 contact@example.com"),
		CrawlRateLimit:   time.Duration(getenvInt("CRAWL_RATE_LIMIT_MS", 1000)) * time.Millisecond,
		RequestTimeout:   time.Duration(getenvInt("HTTP_TIMEOUT_SECONDS", 30)) * time.Second,
		LLMTimeout:       time.Duration(getenvInt("LLM_TIMEOUT_SECONDS", 60)) * time.Second,
		DownloadMaxBytes: int64(getenvInt("DOWNLOAD_MAX_BYTES", 80*1024*1024)),
	}
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
