package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port           string
	DatabaseURL    string
	RedisAddr      string
	KIDJWKSURL     string
	KIDIssuer      string
	KIDAudience    string
	KIDAdminRole   string
	AllowedOrigins string
}

func Load() (Config, error) {
	c := Config{
		Port:           env("PORT", "8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		RedisAddr:      env("REDIS_ADDR", "localhost:6379"),
		KIDJWKSURL:     os.Getenv("KID_JWKS_URL"),
		KIDIssuer:      os.Getenv("KID_ISSUER"),
		KIDAudience:    os.Getenv("KID_AUDIENCE"),
		KIDAdminRole:   env("KID_ADMIN_ROLE", "kplay-admin"),
		AllowedOrigins: os.Getenv("ALLOWED_ORIGINS"),
	}
	var missing []string
	for k, v := range map[string]string{"DATABASE_URL": c.DatabaseURL, "KID_JWKS_URL": c.KIDJWKSURL, "KID_ISSUER": c.KIDIssuer, "KID_AUDIENCE": c.KIDAudience} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing env: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
