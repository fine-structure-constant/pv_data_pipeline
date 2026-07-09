package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadYAMLConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte(`
database:
  dsn: postgres://user:pass@localhost:5432/db?sslmode=disable
storage:
  root: /data/papers
llm:
  provider: openai_or_compatible
  base_url: https://example.test/v1
  api_key: secret
  model: test-model
  timeout_seconds: 12
http:
  user_agent: TestAgent/1.0
  request_timeout_seconds: 7
crawl:
  rate_limit_ms: 250
download:
  max_bytes: 12345
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseDSN != "postgres://user:pass@localhost:5432/db?sslmode=disable" {
		t.Fatalf("unexpected database dsn: %s", cfg.DatabaseDSN)
	}
	if cfg.StorageRoot != "/data/papers" || cfg.LLMProvider != "openai_or_compatible" || cfg.LLMAPIKey != "secret" {
		t.Fatalf("unexpected string config: %#v", cfg)
	}
	if cfg.LLMBaseURL != "https://example.test/v1" || cfg.LLMModel != "test-model" || cfg.HTTPUserAgent != "TestAgent/1.0" {
		t.Fatalf("unexpected service config: %#v", cfg)
	}
	if cfg.LLMTimeout != 12*time.Second || cfg.RequestTimeout != 7*time.Second || cfg.CrawlRateLimit != 250*time.Millisecond {
		t.Fatalf("unexpected duration config: %#v", cfg)
	}
	if cfg.DownloadMaxBytes != 12345 {
		t.Fatalf("unexpected max bytes: %d", cfg.DownloadMaxBytes)
	}
}

func TestLoadMissingConfigUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseDSN == "" || cfg.LLMModel == "" || cfg.RequestTimeout == 0 {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
}
