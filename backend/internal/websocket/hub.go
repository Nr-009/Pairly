package websocket

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	mongoRepo "parily.dev/app/internal/mongo"
	"parily.dev/app/internal/redis"
)

const (
	saveInterval = 60 * time.Second
	saveOpCount  = 100
)

type client struct {
	conn   *websocket.Conn
	roomID string
	fileID string
}

type Hub struct {
	mu sync.RWMutex
	// key is "roomId:fileId"
	rooms   map[string]map[*websocket.Conn]bool
	subs    map[string]*redis.Subscription
	opCount map[string]int
	docRepo *mongoRepo.DocumentRepository
	rdb     *redis.Client
	log     *zap.Logger
}

func NewHub(rdb *redis.Client, docRepo *mongoRepo.DocumentRepository, log *zap.Logger) *Hub {
	h := &Hub{
		rooms:   make(map[string]map[*websocket.Conn]bool),
		subs:    make(map[string]*redis.Subscription),
		opCount: make(map[string]int),
		docRepo: docRepo,
		rdb:     rdb,
		log:     log,
	}
	go h.periodicSave()
	return h
}

func channelKey(roomID, fileID string) string {
	return "room:" + roomID + ":file:" + fileID
}

// LoadState fetches the Yjs state from MongoDB for a file.
// Returns nil if no state exists yet.
func (h *Hub) LoadState(ctx context.Context, fileID string) ([]byte, error) {
	doc, err := h.docRepo.LoadDocument(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if doc == nil || len(doc.YjsState) == 0 {
		return nil, nil
	}
	return doc.YjsState, nil
}

func (h *Hub) Register(conn *websocket.Conn, roomID, fileID string) {
	key := channelKey(roomID, fileID)
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[key] == nil {
		h.rooms[key] = make(map[*websocket.Conn]bool)
	}
	h.rooms[key][conn] = true

	if h.subs[key] == nil {
		h.subscribeRedis(key)
	}
}

func (h *Hub) Unregister(conn *websocket.Conn, roomID, fileID string) {
	key := channelKey(roomID, fileID)
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.rooms[key], conn)

	if len(h.rooms[key]) == 0 {
		// Last user left — save to MongoDB then clean up
		go h.saveNow(context.Background(), fileID, key)

		if h.subs[key] != nil {
			h.subs[key].Close()
			delete(h.subs, key)
		}
		delete(h.rooms, key)
		delete(h.opCount, key)
	}
}

func (h *Hub) Broadcast(sender *websocket.Conn, roomID, fileID string, msgType int, data []byte) {
	key := channelKey(roomID, fileID)

	if err := h.rdb.Publish(key, data); err != nil {
		h.log.Error("redis publish failed", zap.Error(err))
	}

	h.mu.Lock()
	h.opCount[key]++
	count := h.opCount[key]
	h.mu.Unlock()

	// Save every 100 operations
	if count >= saveOpCount {
		h.mu.Lock()
		h.opCount[key] = 0
		h.mu.Unlock()
		go h.saveNow(context.Background(), fileID, key)
	}
}

func (h *Hub) saveNow(ctx context.Context, fileID, key string) {
	h.mu.RLock()
	conns := h.rooms[key]
	h.mu.RUnlock()

	if len(conns) == 0 {
		return
	}

	// Ask one client for the current Yjs state by sending a sync request
	// For now we track state in memory on broadcast — full implementation
	// sends a Yjs sync step 1 message and waits for step 2
	// This is wired properly in Phase 3 with full Yjs awareness
	_ = fileID
}

// periodicSave saves all active documents every 60 seconds
func (h *Hub) periodicSave() {
	ticker := time.NewTicker(saveInterval)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.RLock()
		keys := make([]string, 0, len(h.rooms))
		for k := range h.rooms {
			keys = append(keys, k)
		}
		h.mu.RUnlock()

		for _, key := range keys {
			h.log.Debug("periodic save tick", zap.String("key", key))
		}
	}
}

func (h *Hub) subscribeRedis(key string) {
	sub, err := h.rdb.Subscribe(key)
	if err != nil {
		h.log.Error("redis subscribe failed", zap.String("key", key), zap.Error(err))
		return
	}
	h.subs[key] = sub

	go func() {
		for msg := range sub.Messages() {
			h.mu.RLock()
			conns := h.rooms[key]
			h.mu.RUnlock()

			for conn := range conns {
				if err := conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
					conn.Close()
				}
			}
		}
	}()
}
