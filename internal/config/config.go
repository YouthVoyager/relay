package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Addr 是 HTTP 服务监听地址,如 ":8080"
	Addr string
	DatabaseURL     string
	// 兼容OpenAI接口,用于调用 LLM API
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string
	CompactionThreshold int
	APIToken string
}

// Load 从环境变量读取配置。必填项缺失时返回错误(fail fast)。
func Load() (Config, error) {
	_ = godotenv.Load()
	cfg := Config{
		Addr:            getEnv("RELAY_ADDR", ":8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://relay:relay@localhost:5432/relay?sslmode=disable"),
		LLMBaseURL: getEnv("LLM_BASE_URL", ""),
		LLMAPIKey:  os.Getenv("LLM_API_KEY"),
		LLMModel:   getEnv("LLM_MODEL", ""),
		CompactionThreshold: getEnvInt("RELAY_COMPACTION_THRESHOLD", 20_000),
		APIToken: getEnv("API_TOKEN",""),
	}

	// 校验:现在先不强制要求 API key(阶段 3 才用到),
	// 但地址不能为空这类基本校验从现在就养成习惯
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("RELAY_ADDR must not be empty")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	if cfg.CompactionThreshold < 1000 {
		return Config{}, fmt.Errorf("RELAY_COMPACTION_THRESHOLD too small (min 1000), got %d", cfg.CompactionThreshold)
	}

	return cfg, nil
}

// getEnvInt 读取整数环境变量,缺失或解析失败时返回默认值
func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// getEnv 读取环境变量,为空时返回默认值
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}