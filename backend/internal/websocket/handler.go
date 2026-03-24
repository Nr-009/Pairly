package websocket

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	gorws "github.com/gorilla/websocket"
	"go.uber.org/zap"

	"parily.dev/app/internal/logger"
)

var upgrader = gorws.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Handler struct {
	hub *Hub
}

func NewHandler(hub *Hub) *Handler {
	return &Handler{hub: hub}
}

func (h *Handler) ServeWS(c *gin.Context) {
	roomID := c.Param("room")
	if roomID == "" {
    	roomID = "test"
	}	
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Log.Error("ws: upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	logger.Log.Info("ws: new connection",
		zap.String("room", roomID),
		zap.String("remote", conn.RemoteAddr().String()),
	)

	h.hub.Register(roomID, conn)
	defer h.hub.Unregister(roomID, conn)

	ctx := context.Background()

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			if gorws.IsUnexpectedCloseError(err, gorws.CloseGoingAway, gorws.CloseNormalClosure) {
				logger.Log.Warn("ws: unexpected close", zap.Error(err))
			}
			return
		}

		if msgType != gorws.BinaryMessage {
			continue
		}

		h.hub.Broadcast(ctx, roomID, msg)
	}
}
