package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database      DatabaseConfig      `yaml:"database"`
	Library       LibraryConfig       `yaml:"library"`
	Log           LogConfig           `yaml:"log"`
	Observability ObservabilityConfig `yaml:"observability"`
	Segment       SegmentConfig       `yaml:"segment"`
	Search        SearchConfig        `yaml:"search"`
	Models        ModelsConfig        `yaml:"models"`
	MCP           MCPConfig           `yaml:"mcp"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type LibraryConfig struct {
	MarkdownDir string `yaml:"markdown_dir"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type ObservabilityConfig struct {
	ServiceName    string      `yaml:"service_name"`
	ServiceVersion string      `yaml:"service_version"`
	Trace          TraceConfig `yaml:"trace"`
}

type TraceConfig struct {
	Enabled     bool    `yaml:"enabled"`
	Exporter    string  `yaml:"exporter"`
	Endpoint    string  `yaml:"endpoint"`
	Insecure    bool    `yaml:"insecure"`
	SampleRatio float64 `yaml:"sample_ratio"`
}

// SegmentConfig 控制 gse 分词。
type SegmentConfig struct {
	Dictionaries []string `yaml:"dictionaries"`
}

type SearchConfig struct {
	DefaultLimit   int              `yaml:"default_limit"`
	CandidateLimit int              `yaml:"candidate_limit"`
	MinScore       float64          `yaml:"min_score"`
	RRFWeights     RRFWeightsConfig `yaml:"rrf_weights"`

	EnableVector bool `yaml:"enable_vector"`
	EnableRerank bool `yaml:"enable_rerank"`
}

// RRFWeightsConfig configures per-retrieval-type RRF contribution weights.
type RRFWeightsConfig struct {
	OriginalVector     float64 `yaml:"original_vector"`
	OriginalBM25       float64 `yaml:"original_bm25"`
	RewrittenVector    float64 `yaml:"rewritten_vector"`
	RewrittenBM25      float64 `yaml:"rewritten_bm25"`
	HypotheticalAnswer float64 `yaml:"hypothetical_answer"`
}

type ModelsConfig struct {
	Embedding ModelConfig `yaml:"embedding"`
	Rerank    ModelConfig `yaml:"rerank"`
}

type ModelConfig struct {
	Provider   string            `yaml:"provider"`
	BaseURL    string            `yaml:"base_url"`
	Model      string            `yaml:"model"`
	APIKey     string            `yaml:"api_key"`
	TimeoutMS  int               `yaml:"timeout_ms"`
	Dimensions int               `yaml:"dimensions"`
	Headers    map[string]string `yaml:"headers"`
}

type MCPConfig struct {
	Address          string     `yaml:"address"`
	Path             string     `yaml:"path"`
	JSONResponse     bool       `yaml:"json_response"`
	Stateless        bool       `yaml:"stateless"`
	SessionTimeoutMS int        `yaml:"session_timeout_ms"`
	AllowImport      bool       `yaml:"allow_import"`
	Auth             AuthConfig `yaml:"auth"`
	CORS             CORSConfig `yaml:"cors"`
}

type AuthConfig struct {
	Enabled bool   `yaml:"enabled"`
	Secret  string `yaml:"secret"`
}

type CORSConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	ExposedHeaders   []string `yaml:"exposed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAgeSeconds    int      `yaml:"max_age_seconds"`
}

func Load(path string) (*Config, error) {
	cfg := Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func LogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
