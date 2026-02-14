package natsbus

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mtzanidakis/praktor/internal/config"
	natsserver "github.com/nats-io/nats-server/v2/server"
)

type Bus struct {
	server *natsserver.Server
	cfg    config.NATSConfig
}

func New(cfg config.NATSConfig) (*Bus, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create nats data dir: %w", err)
	}

	opts := &natsserver.Options{
		Port:      cfg.Port,
		NoLog:     true,
		NoSigs:    true,
		JetStream: true,
		StoreDir:  cfg.DataDir,
	}

	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("create nats server: %w", err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		return nil, fmt.Errorf("nats server not ready")
	}

	return &Bus{
		server: ns,
		cfg:    cfg,
	}, nil
}

func (b *Bus) ClientURL() string {
	return b.server.ClientURL()
}

func (b *Bus) Port() int {
	return b.cfg.Port
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
	url := fmt.Sprintf("nats://%s:%d", host, b.cfg.Port)
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
