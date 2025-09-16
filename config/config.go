package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml"
)

type Config struct {
	Logging LoggingConfig `toml:"logging"`
	Bot     BotConfig     `toml:"bot"`
	Auth    AuthConfig    `toml:"auth"`
	Server  ServerConfig  `toml:"server"`
}

type LoggingConfig struct {
	Level                string `toml:"level"`
	EnableFileLogging    bool   `toml:"enable_file_logging"`
	LogFile              string `toml:"log_file"`
	Format               string `toml:"format"`
	IncludeTimestamp     bool   `toml:"include_timestamp"`
	IncludePacketDetails bool   `toml:"include_packet_details"`
}

type BotConfig struct {
	AutoReconnect           bool     `toml:"auto_reconnect"`
	MaxReconnectAttempts    int      `toml:"max_reconnect_attempts"`
	ReconnectDelaySeconds   int      `toml:"reconnect_delay_seconds"`
	Owners                  []string `toml:"owners"`
	Ads                     []string `toml:"ads"`
}

type AuthConfig struct {
	TokenCacheFile string `toml:"token_cache_file"`
}

type ServerConfig struct {
	Address string `toml:"address"`
	Port    int    `toml:"port"`
}

var DefaultConfig = Config{
	Logging: LoggingConfig{
		Level:                "info",
		EnableFileLogging:    false,
		LogFile:              "logs/bot.log",
		Format:               "text",
		IncludeTimestamp:     true,
		IncludePacketDetails: false,
	},
	Bot: BotConfig{
		AutoReconnect:           true,
		MaxReconnectAttempts:    5,
		ReconnectDelaySeconds:   30,
		Owners:                  []string{},
		Ads:                     []string{},
	},
	Auth: AuthConfig{
		TokenCacheFile: "profile/cache.json",
	},
	Server: ServerConfig{
		Address: "bedrock.mcfallout.net",
		Port:    19132,
	},
}

func Load(configPath string) (*Config, error) {
	// Use default config if file doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("Config file not found at %s, using defaults\n", configPath)
		return &DefaultConfig, nil
	}

	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse TOML
	config := DefaultConfig // Start with defaults
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

func LoadDefault() *Config {
	config, _ := Load("config/config.toml")
	return config
}

func validateConfig(config *Config) error {
	// Validate log level
	if config.Logging.Level != "debug" && config.Logging.Level != "info" {
		return fmt.Errorf("log level must be 'debug' or 'info', got '%s'", config.Logging.Level)
	}

	// Validate log format
	if config.Logging.Format != "text" && config.Logging.Format != "json" {
		return fmt.Errorf("log format must be 'text' or 'json', got '%s'", config.Logging.Format)
	}

	// Create logs directory if file logging is enabled
	if config.Logging.EnableFileLogging {
		logDir := filepath.Dir(config.Logging.LogFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	// Validate bot config
	if config.Bot.MaxReconnectAttempts < 0 {
		return fmt.Errorf("max_reconnect_attempts must be >= 0")
	}

	if config.Bot.ReconnectDelaySeconds < 0 {
		return fmt.Errorf("reconnect_delay_seconds must be >= 0")
	}

	// Validate auth config
	if config.Auth.TokenCacheFile == "" {
		return fmt.Errorf("token_cache_file cannot be empty")
	}

	// Validate server config
	if config.Server.Address == "" {
		return fmt.Errorf("server address cannot be empty")
	}

	if config.Server.Port <= 0 || config.Server.Port > 65535 {
		return fmt.Errorf("server port must be between 1 and 65535")
	}

	return nil
}

// IsDebugLevel returns true if logging level is debug
func (c *Config) IsDebugLevel() bool {
	return c.Logging.Level == "debug"
}

// IsInfoLevel returns true if logging level is info
func (c *Config) IsInfoLevel() bool {
	return c.Logging.Level == "info"
}

// ShouldLogToFile returns true if file logging is enabled
func (c *Config) ShouldLogToFile() bool {
	return c.Logging.EnableFileLogging
}

// GetServerAddress returns the full server address in "host:port" format
func (c *Config) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Address, c.Server.Port)
}

// IsOwner checks if the given user is in the owners list
func (c *Config) IsOwner(username string) bool {
	for _, owner := range c.Bot.Owners {
		if owner == username {
			return true
		}
	}
	return false
}