package websocket

import (
	"context"
	"sync"
	gorws "github.com/gorilla/websocket"
	"go.uber.org/zap"
	"parily.dev/app/internal/logger"
	redisclient "parily.dev/app/internal/redis"
)

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*gorws.Conn]bool
	subs  map[string]*redisclient.Subscription
	redis *redisclient.Client
}

func NewHub(redis *redisclient.Client) *Hub {
	return &Hub{
		rooms: make(map[string]map[*gorws.Conn]bool),
		subs:  make(map[string]*redisclient.Subscription),
		redis: redis,
	}
}

func (h *Hub) Register(roomID string, conn *gorws.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[*gorws.Conn]bool)
		h.subscribeRedis(roomID)
	}

	h.rooms[roomID][conn] = true
	logger.Log.Info("ws: client joined",
		zap.String("room", roomID),
		zap.Int("connections", len(h.rooms[roomID])),
	)
}

func (h *Hub) Unregister(roomID string, conn *gorws.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.rooms[roomID], conn)
	logger.Log.Info("ws: client left",
		zap.String("room", roomID),
		zap.Int("remaining", len(h.rooms[roomID])),
	)

	if len(h.rooms[roomID]) == 0 {
		delete(h.rooms, roomID)
		h.unsubscribeRedis(roomID)
	}
}

func (h *Hub) Broadcast(ctx context.Context, roomID string, msg []byte) {
	if err := h.redis.Publish(ctx, roomID, msg); err != nil {
		logger.Log.Error("ws: redis publish failed",
			zap.String("room", roomID),
			zap.Error(err),
		)
	}
}

func (h *Hub) sendToRoom(roomID string, skip *gorws.Conn, msg []byte) {
	h.mu.RLock()
	conns := make([]*gorws.Conn, 0, len(h.rooms[roomID]))
	for conn := range h.rooms[roomID] {
		if conn != skip {
			conns = append(conns, conn)
		}
	}
	h.mu.RUnlock()

	for _, conn := range conns {
		if err := conn.WriteMessage(gorws.BinaryMessage, msg); err != nil {
			logger.Log.Warn("ws: write to client failed", zap.Error(err))
		}
	}
}

func (h *Hub) subscribeRedis(roomID string) {
	sub := h.redis.Subscribe(context.Background(), roomID)
	h.subs[roomID] = sub
	logger.Log.Info("redis: subscribed", zap.String("room", roomID))

	go func() {
		for msg := range sub.Channel() {
			h.sendToRoom(roomID, nil, []byte(msg.Payload))
		}
	}()
}

func (h *Hub) unsubscribeRedis(roomID string) {
	if sub, ok := h.subs[roomID]; ok {
		sub.Close()
		delete(h.subs, roomID)
		logger.Log.Info("redis: unsubscribed", zap.String("room", roomID))
	}
}
