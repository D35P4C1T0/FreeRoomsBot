package bot

import (
	"encoding/json"
	"errors"
	"os"
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
	} `json:"Database"`
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
	if v := os.Getenv("Logging__LogLevel__Default"); v != "" {
		if cfg.Logging.LogLevel == nil {
			cfg.Logging.LogLevel = map[string]string{}
		}
		cfg.Logging.LogLevel["Default"] = v
	}

	if cfg.Bot.BotToken == "" {
		return cfg, errors.New("BotConfiguration.BotToken must be a non-empty string")
	}
	if cfg.Database.ConnectionString == "" {
		return cfg, errors.New("DatabaseConfiguration.ConnectionString must be a non-empty string")
	}

	return cfg, nil
}
