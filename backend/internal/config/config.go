// Package config reads all settings from environment variables (NFR-7).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Config holds every setting the server needs.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string
	UploadDir   string
	BaseURL     string
	// GuestTicketLimit is guest tickets per IP per 10 minutes (NFR-3); GUEST_TICKET_LIMIT, default 5.
	GuestTicketLimit int
}

// Load reads the config through getenv (os.Getenv in main) and reports every problem at once.
func Load(getenv func(string) string) (Config, error) {
	var missing []string
	req := func(name string) string {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			missing = append(missing, name)
		}
		return v
	}

	cfg := Config{
		HTTPAddr:    getenv("HTTP_ADDR"),
		DatabaseURL: req("DATABASE_URL"),
		RedisURL:    req("REDIS_URL"),
		UploadDir:   req("UPLOAD_DIR"),
		BaseURL:     req("BASE_URL"),
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	var errs []error
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", ")))
	}
	cfg.GuestTicketLimit = 5
	if v := strings.TrimSpace(getenv("GUEST_TICKET_LIMIT")); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("GUEST_TICKET_LIMIT must be a positive integer, got %q", v))
		} else {
			cfg.GuestTicketLimit = n
		}
	}
	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		switch {
		case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
			errs = append(errs, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", cfg.BaseURL))
		case u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1":
			// Session cookies get the Secure flag from an https BASE_URL (NFR-1); plain http is for local development.
			errs = append(errs, fmt.Errorf("BASE_URL must use https except on localhost, got %q", cfg.BaseURL))
		}
	}
	return cfg, errors.Join(errs...)
}
