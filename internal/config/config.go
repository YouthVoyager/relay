package config

import (
	"fmt"
	"os"
)

type Config struct {
	// Addr 是 HTTP 服务监听地址,如 ":8080"
	Addr string
	// DBPath 是 SQLite 数据库文件路径
	DBPath string
	// AnthropicAPIKey 用于调用 LLM API
	AnthropicAPIKey string
}

// Load 从环境变量读取配置。必填项缺失时返回错误(fail fast)。
func Load() (Config, error) {
	cfg := Config{
		Addr:            getEnv("RELAY_ADDR", ":8080"),
		DBPath:          getEnv("RELAY_DB_PATH", "relay.db"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
	}

	// 校验:现在先不强制要求 API key(阶段 3 才用到),
	// 但地址不能为空这类基本校验从现在就养成习惯
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("RELAY_ADDR must not be empty")
	}

	return cfg, nil
}

// getEnv 读取环境变量,为空时返回默认值
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}