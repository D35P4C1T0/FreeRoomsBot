package bot

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
)

// Config contains runtime settings loaded from appsettings-style JSON and
// environment variable overrides.
type Config struct {
	// Logging contains application log-level settings.
	Logging struct {
		// LogLevel maps category names to configured log levels.
		LogLevel map[string]string `json:"LogLevel"`
	} `json:"Logging"`
	// Bot contains Telegram bot identity and authentication settings.
	Bot struct {
		// BotToken is the Telegram Bot API token.
		BotToken string `json:"BotToken"`
		// BotName is the display name used in help and start messages.
		BotName string `json:"BotName"`
	} `json:"Bot"`
	// Database contains MongoDB connection settings.
	Database struct {
		// ConnectionString is the MongoDB connection URI.
		ConnectionString string `json:"ConnectionString"`
		// LogRetentionDays controls automatic expiry of usage logs.
		LogRetentionDays int `json:"LogRetentionDays"`
	} `json:"Database"`
	// Health contains local health-check endpoint settings.
	Health struct {
		// Port is the localhost TCP port used by the health-check endpoint.
		Port int `json:"Port"`
	} `json:"Health"`
}

// LoadConfig reads configuration from appsettings.json, or APPSETTINGS_PATH
// when set, and applies supported environment variable overrides.
//
// A missing config file is allowed so deployments can rely entirely on
// environment variables. Other file and JSON errors are returned.
func LoadConfig() (Config, error) {
	cfg := Config{}
	cfg.Bot.BotName = "Free Classrooms Bot - UNITN"
	cfg.Database.ConnectionString = "mongodb://localhost:27017"
	cfg.Database.LogRetentionDays = 90
	cfg.Health.Port = 8080
	cfg.Logging.LogLevel = map[string]string{"Default": "Warning"}

	configPath := "appsettings.json"
	if v := os.Getenv("APPSETTINGS_PATH"); v != "" {
		configPath = v
	}
	if b, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}

	if v := os.Getenv("Bot__BotToken"); v != "" {
		cfg.Bot.BotToken = v
	}
	if v := os.Getenv("Bot__BotName"); v != "" {
		cfg.Bot.BotName = v
	}
	if v := os.Getenv("Database__ConnectionString"); v != "" {
		cfg.Database.ConnectionString = v
	}
	if v := os.Getenv("Database__LogRetentionDays"); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil {
			return cfg, err
		}
		cfg.Database.LogRetentionDays = days
	}
	if v := os.Getenv("Logging__LogLevel__Default"); v != "" {
		if cfg.Logging.LogLevel == nil {
			cfg.Logging.LogLevel = map[string]string{}
		}
		cfg.Logging.LogLevel["Default"] = v
	}
	if v := os.Getenv("Health__Port"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return cfg, err
		}
		cfg.Health.Port = port
	}

	if cfg.Bot.BotToken == "" {
		return cfg, errors.New("BotConfiguration.BotToken must be a non-empty string")
	}
	if cfg.Database.ConnectionString == "" {
		return cfg, errors.New("DatabaseConfiguration.ConnectionString must be a non-empty string")
	}
	if cfg.Database.LogRetentionDays < 1 || cfg.Database.LogRetentionDays > 24855 {
		return cfg, errors.New("Database.LogRetentionDays must be between 1 and 24855")
	}
	if cfg.Health.Port < 1 || cfg.Health.Port > 65535 {
		return cfg, errors.New("Health.Port must be between 1 and 65535")
	}

	return cfg, nil
}
