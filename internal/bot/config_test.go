package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigReadsAppsettingsAndEnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	appsettings := `{
		"Logging": {"LogLevel": {"Default": "Debug"}},
		"Bot": {"BotToken": "from-file", "BotName": "FromFile"},
		"Database": {"ConnectionString": "mongodb://file:27017"}
	}`
	if err := os.WriteFile(filepath.Join(tmp, "appsettings.json"), []byte(appsettings), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("Bot__BotToken", "from-env")
	t.Setenv("Bot__BotName", "FromEnv")
	t.Setenv("Database__ConnectionString", "mongodb://env:27017")
	t.Setenv("Health__Port", "9090")
	t.Setenv("Logging__LogLevel__Default", "Warning")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.BotToken != "from-env" {
		t.Fatalf("BotToken = %q, want from-env", cfg.Bot.BotToken)
	}
	if cfg.Bot.BotName != "FromEnv" {
		t.Fatalf("BotName = %q, want FromEnv", cfg.Bot.BotName)
	}
	if cfg.Database.ConnectionString != "mongodb://env:27017" {
		t.Fatalf("ConnectionString = %q, want env value", cfg.Database.ConnectionString)
	}
	if cfg.Health.Port != 9090 {
		t.Fatalf("Health.Port = %d, want 9090", cfg.Health.Port)
	}
	if cfg.Logging.LogLevel["Default"] != "Warning" {
		t.Fatalf("Default log level = %q, want Warning", cfg.Logging.LogLevel["Default"])
	}
}

func TestLoadConfigUsesAppsettingsPath(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "custom-appsettings.json")
	appsettings := `{
		"Bot": {"BotToken": "from-custom-file"},
		"Database": {"ConnectionString": "mongodb://custom:27017"}
	}`
	if err := os.WriteFile(configPath, []byte(appsettings), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APPSETTINGS_PATH", configPath)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.BotToken != "from-custom-file" {
		t.Fatalf("BotToken = %q, want from-custom-file", cfg.Bot.BotToken)
	}
	if cfg.Database.ConnectionString != "mongodb://custom:27017" {
		t.Fatalf("ConnectionString = %q, want custom value", cfg.Database.ConnectionString)
	}
}

func TestLoadConfigRequiresBotToken(t *testing.T) {
	t.Setenv("Bot__BotToken", "")
	t.Setenv("Bot__BotName", "")
	t.Setenv("Database__ConnectionString", "")

	tmp := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	_, err = LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig succeeded without BotToken")
	}
}

func TestLoadConfigRejectsInvalidHealthPort(t *testing.T) {
	t.Setenv("Bot__BotToken", "token")
	t.Setenv("Health__Port", "70000")

	tmp := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	_, err = LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig succeeded with invalid health port")
	}
}
