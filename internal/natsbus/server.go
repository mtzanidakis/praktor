package natsbus

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/mtzanidakis/praktor/internal/config"
	natsserver "github.com/nats-io/nats-server/v2/server"
)

type Bus struct {
	server *natsserver.Server
	cfg    config.NATSConfig
	port   int
}

func New(cfg config.NATSConfig) (*Bus, error) {
	return newBus(cfg, config.NATSPort)
}

// NewForTest creates a Bus on a random port for testing.
func NewForTest(cfg config.NATSConfig) (*Bus, error) {
	return newBus(cfg, 0)
}

func newBus(cfg config.NATSConfig, port int) (*Bus, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create nats data dir: %w", err)
	}

	opts := &natsserver.Options{
		Port:       port,
		NoLog:      true,
		NoSigs:     true,
		JetStream:  true,
		StoreDir:   cfg.DataDir,
		MaxPayload: 16 << 20, // 16MB for file transfers
	}

	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("create nats server: %w", err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		return nil, fmt.Errorf("nats server not ready")
	}

	// Resolve actual port (may differ from requested when port=0)
	actualPort := ns.Addr().(*net.TCPAddr).Port

	return &Bus{
		server: ns,
		cfg:    cfg,
		port:   actualPort,
	}, nil
}

func (b *Bus) ClientURL() string {
	return b.server.ClientURL()
}

func (b *Bus) Port() int {
	return b.port
}

// AgentNATSURL returns the NATS URL that agent containers should use.
// When the gateway runs inside Docker, it uses the container hostname;
// otherwise it falls back to localhost.
func (b *Bus) AgentNATSURL() string {
	host := "localhost"
	if _, err := os.Stat("/.dockerenv"); err == nil {
		// Running inside Docker — use hostname which is resolvable
		// from other containers on the same network.
		if h, err := os.Hostname(); err == nil && h != "" {
			host = h
		}
	}
	url := fmt.Sprintf("nats://%s:%d", host, b.port)
	slog.Info("agent NATS URL resolved", "url", url)
	return url
}

// NumClients returns the number of connected NATS clients.
// The gateway itself is always one client; agent containers are additional.
func (b *Bus) NumClients() int {
	return int(b.server.NumClients())
}

func (b *Bus) Close() {
	b.server.Shutdown()
	b.server.WaitForShutdown()
}
