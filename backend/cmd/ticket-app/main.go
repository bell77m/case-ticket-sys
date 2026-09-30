package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"ticket-app/internal/api"
	"ticket-app/internal/auth"
	"ticket-app/internal/autoclose"
	"ticket-app/internal/config"
	"ticket-app/internal/models"
	"ticket-app/web"
)

// securityHeaders sets defaults on every response; a handler may override them (evidence downloads set a sandbox CSP).
// script-src and style-src are not here: the built index.html carries them as a <meta> tag with the hash of its inline
// bootstrap script, which changes with each build (kit.csp in frontend/vite.config.ts, T3.17). Browsers enforce both.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func newMux(s *api.Server) http.Handler {
	mux := http.NewServeMux()
	s.Routes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})
	mux.Handle("/", web.Handler())
	return securityHeaders(mux)
}

// newServer cuts off clients that trickle a request (ReadTimeout; uploads extend their own deadline) or hold idle
// connections. There is no WriteTimeout: evidence downloads and server-sent events (T3.04) run long on purpose.
func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "create-root-admin" {
		os.Exit(runCreateRootAdmin(os.Args[2:], os.Getenv))
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	db, err := models.Open(cfg.DatabaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	redisOpt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("invalid REDIS_URL", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpt)
	// Auto-close (FR-T5): once at startup, then hourly. A Redis lock keeps it to one pod per hour.
	go func() {
		tick := time.NewTicker(time.Hour)
		for {
			func() {
				defer func() { // a panic skips this hour instead of killing the server (net/http only recovers handlers)
					if r := recover(); r != nil {
						slog.Error("auto-close panic", "panic", r)
					}
				}()
				n, err := autoclose.Run(context.Background(), db, rdb, time.Now())
				if err != nil {
					slog.Error("auto-close", "error", err)
				}
				if n > 0 {
					slog.Info("auto-closed tickets", "count", n)
				}
			}()
			<-tick.C
		}
	}()
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	app := &api.Server{
		DB:               db,
		UploadDir:        cfg.UploadDir,
		Sessions:         &auth.Sessions{Redis: rdb},
		SecureCookies:    strings.HasPrefix(base, "https://"),
		GuestTicketLimit: cfg.GuestTicketLimit,
		GotenbergURL:     cfg.GotenbergURL,
		PrintBaseURL:     cfg.PrintBaseURL,
		TrustedProxies:   cfg.TrustedProxies,
	}
	srv := newServer(cfg.HTTPAddr, newMux(app))
	slog.Info("listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
