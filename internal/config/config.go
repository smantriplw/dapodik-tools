package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env              string
	Addr             string
	AppName          string
	DatabasePath     string
	SessionSecret    string
	CredentialsKey   string
	SessionSecure    bool
	TrustProxy       bool
	DefaultBaseURL   string
	DefaultSekolahID string
	DefaultNPSN      string
	LogLevel         string
	ShutdownTimeout  time.Duration
}

func Load() (*Config, error) {
	loadDotEnv(".env")

	cfg := &Config{
		Env:              env("APP_ENV", "development"),
		Addr:             env("APP_ADDR", "127.0.0.1:8000"),
		AppName:          env("APP_NAME", "SMANTI Tools"),
		DatabasePath:     env("DATABASE_PATH", "./data/app.db"),
		SessionSecret:    os.Getenv("SESSION_SECRET"),
		SessionSecure:    envBool("SESSION_COOKIE_SECURE"),
		TrustProxy:       envBool("TRUST_PROXY"),
		DefaultBaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("DEFAULT_BASE_URL")), "/"),
		DefaultSekolahID: strings.TrimSpace(os.Getenv("DEFAULT_SEKOLAH_ID")),
		DefaultNPSN:      strings.TrimSpace(os.Getenv("DEFAULT_NPSN")),
		LogLevel:         env("LOG_LEVEL", "info"),
		ShutdownTimeout:  10 * time.Second,
	}

	if cfg.SessionSecret == "" {
		if cfg.Env == "production" {
			return nil, errors.New("SESSION_SECRET wajib diisi pada APP_ENV=production")
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		cfg.SessionSecret = hex.EncodeToString(buf)
	}
	if len(cfg.SessionSecret) < 16 {
		return nil, fmt.Errorf("SESSION_SECRET terlalu pendek (minimal 16 karakter)")
	}
	if cfg.Env == "production" && len(cfg.SessionSecret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET production minimal 32 karakter")
	}

	credentialsKey := strings.TrimSpace(os.Getenv("CREDENTIALS_KEY"))
	if credentialsKey == "" {
		credentialsKey = cfg.SessionSecret
	}
	if len(credentialsKey) < 16 {
		return nil, fmt.Errorf("CREDENTIALS_KEY terlalu pendek (minimal 16 karakter)")
	}
	if cfg.Env == "production" && os.Getenv("CREDENTIALS_KEY") != "" && len(credentialsKey) < 32 {
		return nil, fmt.Errorf("CREDENTIALS_KEY production minimal 32 karakter")
	}
	cfg.CredentialsKey = credentialsKey

	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8000"
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return false
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
