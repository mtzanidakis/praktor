package natsbus

import (
	"fmt"
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

func (b *Bus) Close() {
	b.server.Shutdown()
	b.server.WaitForShutdown()
}
