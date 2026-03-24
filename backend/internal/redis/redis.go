package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"parily.dev/app/internal/config"
)

type Client struct {
	rdb *redis.Client
}

type Subscription struct {
	ps *redis.PubSub
}

func (s *Subscription) Channel() <-chan *redis.Message {
	return s.ps.Channel()
}

func (s *Subscription) Close() {
	_ = s.ps.Close()
}

func Connect(cfg *config.Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       0,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

func (c *Client) Publish(ctx context.Context, roomID string, msg []byte) error {
	return c.rdb.Publish(ctx, "room:"+roomID, msg).Err()
}

func (c *Client) Subscribe(ctx context.Context, roomID string) *Subscription {
	return &Subscription{ps: c.rdb.Subscribe(ctx, "room:"+roomID)}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.rdb.Close()
}
