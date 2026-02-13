package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram  TelegramConfig  `yaml:"telegram"`
	Agent     AgentConfig     `yaml:"agent"`
	NATS      NATSConfig      `yaml:"nats"`
	Store     StoreConfig     `yaml:"store"`
	Web       WebConfig       `yaml:"web"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Groups    GroupsConfig    `yaml:"groups"`
}

type TelegramConfig struct {
	Token     string  `yaml:"token"`
	AllowFrom []int64 `yaml:"allow_from"`
}

type AgentConfig struct {
	Image           string        `yaml:"image"`
	MaxContainers   int           `yaml:"max_containers"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	AnthropicAPIKey string        `yaml:"anthropic_api_key"`
	OAuthToken      string        `yaml:"oauth_token"`
}

type NATSConfig struct {
	Port    int    `yaml:"port"`
	DataDir string `yaml:"data_dir"`
}

type StoreConfig struct {
	Path string `yaml:"path"`
}

type WebConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Auth    string `yaml:"auth"`
}

type SchedulerConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
}

type GroupsConfig struct {
	BasePath   string `yaml:"base_path"`
	MainChatID string `yaml:"main_chat_id"`
}

func defaults() Config {
	return Config{
		Agent: AgentConfig{
			Image:         "praktor-agent:latest",
			MaxContainers: 5,
			IdleTimeout:   30 * time.Minute,
		},
		NATS: NATSConfig{
			Port:    4222,
			DataDir: "data/nats",
		},
		Store: StoreConfig{
			Path: "data/praktor.db",
		},
		Web: WebConfig{
			Enabled: true,
			Port:    8080,
		},
		Scheduler: SchedulerConfig{
			PollInterval: 30 * time.Second,
		},
		Groups: GroupsConfig{
			BasePath: "groups",
		},
	}
}

func Load() (*Config, error) {
	cfg := defaults()

	path := os.Getenv("PRAKTOR_CONFIG")
	if path == "" {
		path = "config/praktor.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found, use defaults + env
	} else {
		// Expand environment variables in YAML
		expanded := os.ExpandEnv(string(data))
		if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	// Environment variable overrides
	applyEnv(&cfg)

	return &cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("PRAKTOR_TELEGRAM_TOKEN"); v != "" {
		cfg.Telegram.Token = v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.Agent.AnthropicAPIKey = v
	}
	if v := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); v != "" {
		cfg.Agent.OAuthToken = v
	}
	if v := os.Getenv("PRAKTOR_WEB_PASSWORD"); v != "" {
		cfg.Web.Auth = v
	}
	if v := os.Getenv("PRAKTOR_WEB_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Web.Port = port
		}
	}
	if v := os.Getenv("PRAKTOR_NATS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.NATS.Port = port
		}
	}
	if v := os.Getenv("PRAKTOR_STORE_PATH"); v != "" {
		cfg.Store.Path = v
	}
	if v := os.Getenv("PRAKTOR_GROUPS_BASE"); v != "" {
		cfg.Groups.BasePath = v
	}
	if v := os.Getenv("PRAKTOR_MAIN_CHAT_ID"); v != "" {
		cfg.Groups.MainChatID = v
	}
}
