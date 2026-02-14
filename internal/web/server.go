package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/swarm"
	"github.com/nats-io/nats.go"
)

//go:embed static
var staticFiles embed.FS

type Server struct {
	store      *store.Store
	bus        *natsbus.Bus
	orch       *agent.Orchestrator
	swarmCoord *swarm.Coordinator
	hub        *Hub
	cfg        config.WebConfig
	startedAt  time.Time
}

func NewServer(s *store.Store, bus *natsbus.Bus, orch *agent.Orchestrator, swarmCoord *swarm.Coordinator, cfg config.WebConfig) *Server {
	return &Server{
		store:      s,
		bus:        bus,
		orch:       orch,
		swarmCoord: swarmCoord,
		hub:        NewHub(),
		cfg:        cfg,
		startedAt:  time.Now(),
	}
}

func (s *Server) Start(ctx context.Context) error {
	go s.hub.Run(ctx)

	// Subscribe to NATS events and broadcast to WebSocket
	s.subscribeEvents()

	mux := http.NewServeMux()

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

		// Basic auth for API routes
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/ws" && s.cfg.Auth != "" {
			_, pass, ok := r.BasicAuth()
			if !ok || pass != s.cfg.Auth {
				w.Header().Set("WWW-Authenticate", `Basic realm="Praktor"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
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
