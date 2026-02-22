package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/router"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/swarm"
	"github.com/mtzanidakis/praktor/internal/vault"
	"github.com/nats-io/nats.go"
)

//go:embed static
var staticFiles embed.FS

const (
	sessionCookieName = "session"
	sessionMaxAge     = 30 * 24 * time.Hour // 30 days
)

type Server struct {
	store      *store.Store
	bus        *natsbus.Bus
	nats       *natsbus.Client
	orch       *agent.Orchestrator
	registry   *registry.Registry
	router     *router.Router
	swarmCoord *swarm.Coordinator
	vault      *vault.Vault
	hub        *Hub
	cfg        config.WebConfig
	version    string
	startedAt  time.Time

	sessionMu sync.Mutex
	sessions  map[string]time.Time // token → expiry
}

func NewServer(s *store.Store, bus *natsbus.Bus, orch *agent.Orchestrator, reg *registry.Registry, rtr *router.Router, swarmCoord *swarm.Coordinator, cfg config.WebConfig, v *vault.Vault, version string) *Server {
	return &Server{
		store:      s,
		bus:        bus,
		orch:       orch,
		registry:   reg,
		router:     rtr,
		swarmCoord: swarmCoord,
		vault:      v,
		hub:        NewHub(),
		cfg:        cfg,
		version:    version,
		startedAt:  time.Now(),
		sessions:   make(map[string]time.Time),
	}
}

func (s *Server) Start(ctx context.Context) error {
	go s.hub.Run(ctx)

	// Subscribe to NATS events and broadcast to WebSocket
	s.subscribeEvents()

	mux := http.NewServeMux()

	// Auth endpoints (public)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/check", s.handleAuthCheck)

	// API routes
	s.registerAPI(mux)

	// WebSocket
	mux.HandleFunc("/api/ws", s.handleWebSocket)

	// SPA static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("static fs: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for non-file routes
		if !strings.Contains(r.URL.Path, ".") && r.URL.Path != "/" {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	handler := s.withMiddleware(mux)
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	server := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	slog.Info("web server listening", "addr", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Session/auth for API routes (except public auth endpoints)
		if strings.HasPrefix(r.URL.Path, "/api/") && s.cfg.Auth != "" {
			// Public endpoints: login and auth check
			if r.URL.Path == "/api/login" || r.URL.Path == "/api/auth/check" {
				next.ServeHTTP(w, r)
				return
			}

			if !s.checkAuth(w, r) {
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// checkAuth validates session cookie or Basic Auth. Returns true if authenticated.
func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	// Check session cookie first
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessionMu.Lock()
		expiry, ok := s.sessions[cookie.Value]
		if ok && time.Now().Before(expiry) {
			// Refresh session expiry
			s.sessions[cookie.Value] = time.Now().Add(sessionMaxAge)
			s.sessionMu.Unlock()
			s.setSessionCookie(w, cookie.Value)
			return true
		}
		// Expired or unknown — clean up
		if ok {
			delete(s.sessions, cookie.Value)
		}
		s.sessionMu.Unlock()
	}

	// Fall back to Basic Auth (for programmatic API access)
	if _, pass, ok := r.BasicAuth(); ok && pass == s.cfg.Auth {
		return true
	}

	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return false
}

func (s *Server) createSession() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	s.sessionMu.Lock()
	s.sessions[token] = time.Now().Add(sessionMaxAge)
	s.sessionMu.Unlock()

	return token, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth == "" {
		jsonResponse(w, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.Password != s.cfg.Auth {
		jsonError(w, "invalid password", http.StatusUnauthorized)
		return
	}

	token, err := s.createSession()
	if err != nil {
		jsonError(w, "session creation failed", http.StatusInternalServerError)
		return
	}

	s.setSessionCookie(w, token)
	jsonResponse(w, map[string]string{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessionMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionMu.Unlock()
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	jsonResponse(w, map[string]string{"status": "ok"})
}

func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	// No auth configured — tell the UI to skip login
	if s.cfg.Auth == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check session cookie
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessionMu.Lock()
		expiry, ok := s.sessions[cookie.Value]
		if ok && time.Now().Before(expiry) {
			s.sessions[cookie.Value] = time.Now().Add(sessionMaxAge)
			s.sessionMu.Unlock()
			s.setSessionCookie(w, cookie.Value)
			jsonResponse(w, map[string]string{"status": "ok"})
			return
		}
		if ok {
			delete(s.sessions, cookie.Value)
		}
		s.sessionMu.Unlock()
	}

	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func (s *Server) subscribeEvents() {
	if s.bus == nil {
		return
	}
	client, err := natsbus.NewClient(s.bus)
	if err != nil {
		slog.Error("web server nats client failed", "error", err)
		return
	}
	s.nats = client

	// Forward all event topics to WebSocket as raw JSON
	_, _ = client.Subscribe(natsbus.TopicEventsAll, func(msg *nats.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			slog.Warn("invalid NATS event payload", "error", err)
			return
		}
		s.hub.Broadcast(event)
	})
}
