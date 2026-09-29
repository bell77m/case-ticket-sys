// Package config reads all settings from environment variables (NFR-7).
package config

import (
	"errors"
	"fmt"
	"net/netip"
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
	// GotenbergURL and PrintBaseURL enable the PDF export (FR-P4); without both it answers 503. PrintBaseURL is where
	// Gotenberg's Chromium reaches the frontend: the Vite server in dev, the app service in the cluster.
	GotenbergURL string
	PrintBaseURL string
	// TrustedProxies are the ingress CIDRs whose X-Forwarded-For is believed (FR-A11, NFR-3); TRUSTED_PROXIES,
	// comma-separated, empty trusts nobody.
	TrustedProxies []netip.Prefix
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
		HTTPAddr:     getenv("HTTP_ADDR"),
		DatabaseURL:  req("DATABASE_URL"),
		RedisURL:     req("REDIS_URL"),
		UploadDir:    req("UPLOAD_DIR"),
		BaseURL:      req("BASE_URL"),
		GotenbergURL: strings.TrimSpace(getenv("GOTENBERG_URL")),
		PrintBaseURL: strings.TrimSpace(getenv("PRINT_BASE_URL")),
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
	for _, v := range strings.Split(getenv("TRUSTED_PROXIES"), ",") {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		if p, err := netip.ParsePrefix(v); err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES must be comma-separated CIDRs, got %q", v))
		} else {
			cfg.TrustedProxies = append(cfg.TrustedProxies, p)
		}
	}
	if cfg.BaseURL != "" {
		u := httpURL(cfg.BaseURL)
		switch {
		case u == nil:
			errs = append(errs, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", cfg.BaseURL))
		case u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1":
			// Session cookies get the Secure flag from an https BASE_URL (NFR-1); plain http is for local development.
			errs = append(errs, fmt.Errorf("BASE_URL must use https except on localhost, got %q", cfg.BaseURL))
		}
	}
	// Plain http is fine here: both are reached inside the cluster, never by a browser (NFR-6).
	for _, v := range [][2]string{{"GOTENBERG_URL", cfg.GotenbergURL}, {"PRINT_BASE_URL", cfg.PrintBaseURL}} {
		if v[1] != "" && httpURL(v[1]) == nil {
			errs = append(errs, fmt.Errorf("%s must be an absolute http(s) URL, got %q", v[0], v[1]))
		}
	}
	return cfg, errors.Join(errs...)
}

// httpURL parses an absolute http(s) URL, or returns nil.
func httpURL(v string) *url.URL {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	return u
}
