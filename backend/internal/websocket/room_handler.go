package websocket

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	pg "parily.dev/app/internal/postgres"
)

type RoomHandler struct {
	hub *RoomHub
	db  *pgxpool.Pool
	cfg *config.Config
	log *zap.Logger
}

func NewRoomHandler(hub *RoomHub, db *pgxpool.Pool, cfg *config.Config, log *zap.Logger) *RoomHandler {
	return &RoomHandler{hub: hub, db: db, cfg: cfg, log: log}
}

// ServeRoom handles GET /ws/:roomId/room
// Validates JWT, checks membership, upgrades to WebSocket, then:
//   - registers the connection in RoomHub
//   - reads incoming messages (heartbeats, future types) and publishes to Redis
//   - Redis fans the message out to all connections in the room via RoomHub
func (h *RoomHandler) ServeRoom(c *gin.Context) {
	roomID := c.Param("roomId")

	claims, err := auth.ParseToken(c, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	_, err = pg.GetMemberRole(c.Request.Context(), h.db, roomID, claims.UserID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("room ws upgrade failed", zap.Error(err))
		return
	}

	h.hub.Register(conn, roomID)
	defer h.hub.Unregister(conn, roomID)

	h.log.Info("room ws connected",
		zap.String("room", roomID),
		zap.String("user", claims.UserID),
	)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if err := h.hub.Publish(roomID, data); err != nil {
			h.log.Error("room hub publish failed",
				zap.String("room", roomID),
				zap.String("user", claims.UserID),
				zap.Error(err),
			)
		}
	}
}
