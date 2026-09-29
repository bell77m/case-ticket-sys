package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://ticket:ticket@localhost:5432/ticket",
		"REDIS_URL":    "redis://localhost:6379/0",
		"UPLOAD_DIR":   "./uploads",
		"BASE_URL":     "http://localhost:5173",
	}
}

// NFR-7: all secrets and settings come from environment variables.
func TestLoad_Valid_NFR7(t *testing.T) {
	cfg, err := Load(env(valid()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want default :8080", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL != valid()["DATABASE_URL"] || cfg.BaseURL != "http://localhost:5173" {
		t.Errorf("values not copied: %+v", cfg)
	}
	if cfg.GuestTicketLimit != 5 {
		t.Errorf("GuestTicketLimit = %d, want default 5 (NFR-3)", cfg.GuestTicketLimit)
	}
	m := valid()
	m["GUEST_TICKET_LIMIT"] = "1000"
	if cfg, err := Load(env(m)); err != nil || cfg.GuestTicketLimit != 1000 {
		t.Errorf("GUEST_TICKET_LIMIT=1000: limit %d, error %v", cfg.GuestTicketLimit, err)
	}
}

func TestLoad_Invalid_NFR7(t *testing.T) {
	tests := []struct {
		name string
		edit func(m map[string]string)
		want []string
	}{
		{"all missing", func(m map[string]string) { clear(m) },
			[]string{"DATABASE_URL", "REDIS_URL", "UPLOAD_DIR", "BASE_URL"}},
		{"one missing", func(m map[string]string) { delete(m, "REDIS_URL") }, []string{"REDIS_URL"}},
		{"bad base url", func(m map[string]string) { m["BASE_URL"] = "localhost" }, []string{"BASE_URL"}},
		// Session cookies are Secure only on https (NFR-1); plain http is for localhost development.
		{"http base url off localhost", func(m map[string]string) { m["BASE_URL"] = "http://tickets.example.com" }, []string{"BASE_URL"}},
		// NFR-3: the guest ticket limit is a positive integer.
		{"guest limit not a number", func(m map[string]string) { m["GUEST_TICKET_LIMIT"] = "five" }, []string{"GUEST_TICKET_LIMIT"}},
		{"guest limit zero", func(m map[string]string) { m["GUEST_TICKET_LIMIT"] = "0" }, []string{"GUEST_TICKET_LIMIT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid()
			tt.edit(m)
			_, err := Load(env(m))
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			for _, name := range tt.want {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("error %q does not name %s", err, name)
				}
			}
		})
	}
}

// FR-R1: there is no SSO; SSO settings are not needed.
func TestLoad_NoSSO_FRR1(t *testing.T) {
	if _, err := Load(env(valid())); err != nil {
		t.Fatalf("Load() without SSO settings = %v, want no error", err)
	}
}

// NFR-3: an invalid GUEST_TICKET_LIMIT is reported and never stored, so the limit keeps its default of 5 even if a
// caller ignores the error.
func TestLoad_BadGuestLimitKeepsDefault_NFR3(t *testing.T) {
	for _, v := range []string{"0", "-1", "many"} {
		m := valid()
		m["GUEST_TICKET_LIMIT"] = v
		cfg, err := Load(env(m))
		if err == nil || cfg.GuestTicketLimit != 5 {
			t.Errorf("GUEST_TICKET_LIMIT=%q: limit %d, err %v; want 5 and an error", v, cfg.GuestTicketLimit, err)
		}
	}
}

// Notifications are in-app only (T2.12 skipped): SMTP settings are not read, so a lone SMTP_ADDR is no error.
func TestLoad_NoSMTP(t *testing.T) {
	m := valid()
	m["SMTP_ADDR"] = "mail:25"
	if _, err := Load(env(m)); err != nil {
		t.Errorf("Load() with only SMTP_ADDR = %v, want no error", err)
	}
}

// FR-A11, NFR-3: TRUSTED_PROXIES is a comma-separated CIDR list; empty trusts nobody, a bad entry is an error.
func TestLoad_TrustedProxies_FRA11(t *testing.T) {
	tests := []struct {
		name, v string
		want    []string
		wantErr bool
	}{
		{"unset", "", nil, false},
		{"one", "10.0.0.0/8", []string{"10.0.0.0/8"}, false},
		{"list with spaces and IPv6", " 10.0.0.0/8 , fd00::/8,", []string{"10.0.0.0/8", "fd00::/8"}, false},
		{"bare IP", "10.0.0.1", nil, true},
		{"garbage", "10.0.0.0/8,proxy", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid()
			m["TRUSTED_PROXIES"] = tt.v
			cfg, err := Load(env(m))
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
					t.Errorf("Load() error = %v, want one naming TRUSTED_PROXIES", err)
				}
				return
			}
			if err != nil || len(cfg.TrustedProxies) != len(tt.want) {
				t.Fatalf("Load() = %v, %v; want %v", cfg.TrustedProxies, err, tt.want)
			}
			for i, p := range cfg.TrustedProxies {
				if p.String() != tt.want[i] {
					t.Errorf("TrustedProxies[%d] = %s, want %s", i, p, tt.want[i])
				}
			}
		})
	}
}

// FR-P4: the PDF export settings are optional (export answers 503 without them); when set they are absolute http(s) URLs.
func TestLoad_PDF_FRP4(t *testing.T) {
	tests := []struct {
		name, gotenberg, printBase string
		wantErr                    string // "" = valid
	}{
		{"unset", "", "", ""},
		{"dev", "http://localhost:3000", "http://host.docker.internal:5173", ""},
		{"cluster", "http://gotenberg:3000", "https://ticket-app.tickets.svc/", ""},
		{"gotenberg without scheme", "gotenberg:3000", "", "GOTENBERG_URL"},
		{"print base relative", "", "/print", "PRINT_BASE_URL"},
		{"print base not http", "", "ftp://app", "PRINT_BASE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid()
			m["GOTENBERG_URL"], m["PRINT_BASE_URL"] = tt.gotenberg, tt.printBase
			cfg, err := Load(env(m))
			if tt.wantErr == "" {
				if err != nil || cfg.GotenbergURL != tt.gotenberg || cfg.PrintBaseURL != tt.printBase {
					t.Errorf("Load() = %q %q, %v; want the values and no error", cfg.GotenbergURL, cfg.PrintBaseURL, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %v, want one naming %s", err, tt.wantErr)
			}
		})
	}
}
