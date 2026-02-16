package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram  TelegramConfig                `yaml:"telegram"`
	Defaults  DefaultsConfig                `yaml:"defaults"`
	Agents    map[string]AgentDefinition    `yaml:"agents"`
	Router    RouterConfig                  `yaml:"router"`
	NATS      NATSConfig                    `yaml:"nats"`
	Web       WebConfig                     `yaml:"web"`
	Scheduler SchedulerConfig               `yaml:"scheduler"`
}

type TelegramConfig struct {
	Token      string  `yaml:"token"`
	AllowFrom  []int64 `yaml:"allow_from"`
	MainChatID int64   `yaml:"main_chat_id"`
}

type DefaultsConfig struct {
	Image           string        `yaml:"image"`
	Model           string        `yaml:"model"`
	MaxRunning   int           `yaml:"max_running"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	AnthropicAPIKey string        `yaml:"anthropic_api_key"`
	OAuthToken      string        `yaml:"oauth_token"`
}

const (
	AgentsBasePath = "data/agents"
	StorePath      = "data/praktor.db"
)

type AgentDefinition struct {
	Description  string            `yaml:"description"`
	Model        string            `yaml:"model"`
	Image        string            `yaml:"image"`
	ClaudeMD     string            `yaml:"claude_md"`
	Workspace    string            `yaml:"workspace"`
	Env          map[string]string `yaml:"env"`
	Secrets      []string          `yaml:"secrets"`
	AllowedTools []string          `yaml:"allowed_tools"`
}

type RouterConfig struct {
	DefaultAgent string `yaml:"default_agent"`
}

type NATSConfig struct {
	Port    int    `yaml:"port"`
	DataDir string `yaml:"data_dir"`
}


type WebConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Auth    string `yaml:"auth"`
}

type SchedulerConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
}

func defaults() Config {
	return Config{
		Defaults: DefaultsConfig{
			Image:         "praktor-agent:latest",
			Model:         "claude-opus-4-6",
			MaxRunning:  5,
			IdleTimeout: 10 * time.Minute,
		},
		NATS: NATSConfig{
			Port:    4222,
			DataDir: "data/nats",
		},
		Web: WebConfig{
			Enabled: true,
			Port:    8080,
		},
		Scheduler: SchedulerConfig{
			PollInterval: 30 * time.Second,
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

	// Apply defaults for agent definitions
	for name, def := range cfg.Agents {
		if def.Workspace == "" {
			def.Workspace = name
			cfg.Agents[name] = def
		}
	}

	// Validation
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if len(cfg.Agents) > 0 && cfg.Router.DefaultAgent == "" {
		return fmt.Errorf("router.default_agent is required when agents are defined")
	}
	if cfg.Router.DefaultAgent != "" {
		if _, ok := cfg.Agents[cfg.Router.DefaultAgent]; !ok {
			return fmt.Errorf("router.default_agent %q not found in agents map", cfg.Router.DefaultAgent)
		}
	}
	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("PRAKTOR_TELEGRAM_TOKEN"); v != "" {
		cfg.Telegram.Token = v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.Defaults.AnthropicAPIKey = v
	}
	if v := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); v != "" {
		cfg.Defaults.OAuthToken = v
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
	if v := os.Getenv("PRAKTOR_AGENT_MODEL"); v != "" {
		cfg.Defaults.Model = v
	}
}
