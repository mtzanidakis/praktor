package natsbus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type Client struct {
	conn *nats.Conn
}

func NewClient(bus *Bus) (*Client, error) {
	conn, err := nats.Connect(bus.ClientURL())
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	return &Client{conn: conn}, nil
}

func NewClientFromURL(url string) (*Client, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Publish(topic string, data []byte) error {
	return c.conn.Publish(topic, data)
}

func (c *Client) PublishJSON(topic string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return c.conn.Publish(topic, data)
}

func (c *Client) Subscribe(topic string, handler func(msg *nats.Msg)) (*nats.Subscription, error) {
	return c.conn.Subscribe(topic, handler)
}

func (c *Client) Request(topic string, data []byte, timeout time.Duration) (*nats.Msg, error) {
	return c.conn.Request(topic, data, timeout)
}

func (c *Client) Flush() error {
	return c.conn.Flush()
}

func (c *Client) Close() {
	c.conn.Close()
}
