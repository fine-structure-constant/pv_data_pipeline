package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains the runtime configuration used by the pipeline services.
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
	WebAddr          string
}

type fileConfig struct {
	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`
	Storage struct {
		Root string `yaml:"root"`
	} `yaml:"storage"`
	LLM struct {
		Provider       string `yaml:"provider"`
		BaseURL        string `yaml:"base_url"`
		APIKey         string `yaml:"api_key"`
		Model          string `yaml:"model"`
		TimeoutSeconds int    `yaml:"timeout_seconds"`
	} `yaml:"llm"`
	HTTP struct {
		UserAgent             string `yaml:"user_agent"`
		RequestTimeoutSeconds int    `yaml:"request_timeout_seconds"`
	} `yaml:"http"`
	Crawl struct {
		RateLimitMS int `yaml:"rate_limit_ms"`
	} `yaml:"crawl"`
	Download struct {
		MaxBytes int64 `yaml:"max_bytes"`
	} `yaml:"download"`
	Web struct {
		Addr string `yaml:"addr"`
	} `yaml:"web"`
}

// Load reads a YAML configuration file and returns the application's effective configuration.
func Load(path string) (Config, error) {
	cfg := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	var fc fileConfig
	if err := parseFileConfig(b, &fc); err != nil {
		return cfg, err
	}
	applyFileConfig(&cfg, fc)
	return cfg, nil
}

// Defaults returns the built-in settings used when config.yaml omits a value.
func Defaults() Config {
	return Config{
		DatabaseDSN:      "postgres://pvsk_app:password@127.0.0.1:5432/pvsk_db?sslmode=disable",
		StorageRoot:      "/home/rocky/HDDdata/perovskite_papers",
		LLMBaseURL:       "https://api.openai.com/v1",
		LLMModel:         "gpt-5-mini",
		HTTPUserAgent:    "PvskCrawler/0.1 contact@example.com",
		CrawlRateLimit:   time.Second,
		RequestTimeout:   30 * time.Second,
		LLMTimeout:       60 * time.Second,
		DownloadMaxBytes: 80 * 1024 * 1024,
		WebAddr:          ":8080",
	}
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Database.DSN != "" {
		cfg.DatabaseDSN = fc.Database.DSN
	}
	if fc.Storage.Root != "" {
		cfg.StorageRoot = fc.Storage.Root
	}
	if fc.LLM.Provider != "" {
		cfg.LLMProvider = fc.LLM.Provider
	}
	if fc.LLM.BaseURL != "" {
		cfg.LLMBaseURL = fc.LLM.BaseURL
	}
	if fc.LLM.APIKey != "" {
		cfg.LLMAPIKey = fc.LLM.APIKey
	}
	if fc.LLM.Model != "" {
		cfg.LLMModel = fc.LLM.Model
	}
	if fc.LLM.TimeoutSeconds > 0 {
		cfg.LLMTimeout = time.Duration(fc.LLM.TimeoutSeconds) * time.Second
	}
	if fc.HTTP.UserAgent != "" {
		cfg.HTTPUserAgent = fc.HTTP.UserAgent
	}
	if fc.HTTP.RequestTimeoutSeconds > 0 {
		cfg.RequestTimeout = time.Duration(fc.HTTP.RequestTimeoutSeconds) * time.Second
	}
	if fc.Crawl.RateLimitMS > 0 {
		cfg.CrawlRateLimit = time.Duration(fc.Crawl.RateLimitMS) * time.Millisecond
	}
	if fc.Download.MaxBytes > 0 {
		cfg.DownloadMaxBytes = fc.Download.MaxBytes
	}
	if fc.Web.Addr != "" {
		cfg.WebAddr = fc.Web.Addr
	}
}

func parseFileConfig(b []byte, fc *fileConfig) error {
	scanner := bufio.NewScanner(bytes.NewReader(b))
	section := ""
	for lineNo := 1; scanner.Scan(); lineNo++ {
		raw := stripComment(scanner.Text())
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		key, value, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok {
			return fmt.Errorf("line %d: expected key: value", lineNo)
		}
		key = strings.TrimSpace(key)
		value = cleanValue(value)
		if indent == 0 && value == "" {
			section = key
			continue
		}
		if indent == 0 {
			section = ""
		}
		if err := assignValue(fc, section, key, value, lineNo); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func stripComment(s string) string {
	inSingle := false
	inDouble := false
	for i, r := range s {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return s[:i]
			}
		}
	}
	return s
}

func cleanValue(s string) string {
	v := strings.TrimSpace(s)
	v = strings.Trim(v, `"'`)
	return v
}

func assignValue(fc *fileConfig, section, key, value string, lineNo int) error {
	switch section + "." + key {
	case "database.dsn":
		fc.Database.DSN = value
	case "storage.root":
		fc.Storage.Root = value
	case "llm.provider":
		fc.LLM.Provider = value
	case "llm.base_url":
		fc.LLM.BaseURL = value
	case "llm.api_key":
		fc.LLM.APIKey = value
	case "llm.model":
		fc.LLM.Model = value
	case "llm.timeout_seconds":
		n, err := parseInt(value, lineNo)
		if err != nil {
			return err
		}
		fc.LLM.TimeoutSeconds = n
	case "http.user_agent":
		fc.HTTP.UserAgent = value
	case "http.request_timeout_seconds":
		n, err := parseInt(value, lineNo)
		if err != nil {
			return err
		}
		fc.HTTP.RequestTimeoutSeconds = n
	case "crawl.rate_limit_ms":
		n, err := parseInt(value, lineNo)
		if err != nil {
			return err
		}
		fc.Crawl.RateLimitMS = n
	case "download.max_bytes":
		n, err := parseInt64(value, lineNo)
		if err != nil {
			return err
		}
		fc.Download.MaxBytes = n
	case "web.addr":
		fc.Web.Addr = value
	}
	return nil
}

func parseInt(value string, lineNo int) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("line %d: expected integer, got %q", lineNo, value)
	}
	return n, nil
}

func parseInt64(value string, lineNo int) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("line %d: expected integer, got %q", lineNo, value)
	}
	return n, nil
}
