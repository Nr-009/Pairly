package rooms

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"

	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

type Handler struct {
	db      *pgxpool.Pool
	mongoDB *mongo.Database
}

func NewHandler(db *pgxpool.Pool, mongoDB *mongo.Database) *Handler {
	return &Handler{db: db, mongoDB: mongoDB}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("", h.CreateRoom)
	rg.GET("", h.ListRooms)
	rg.GET("/:roomID/role", h.GetRole)
	rg.GET("/:roomID/files", h.GetFiles)
	rg.PATCH("/:roomID/files/:fileID", h.UpdateFile)
	rg.POST("/:roomID/members", h.AddMember)
	rg.POST("/:roomID/files/:fileID/state", h.SaveState)
	rg.GET("/:roomID/files/:fileID/state", h.LoadState)
}

type createRoomRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

func (h *Handler) CreateRoom(c *gin.Context) {
	var req createRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ownerID := c.GetString("userID")
	room, fileID, err := pg.CreateRoom(c.Request.Context(), h.db, strings.TrimSpace(req.Name), ownerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create room"})
		return
	}
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.CreateDocument(c.Request.Context(), fileID, room.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create document"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"room": gin.H{
			"id":         room.ID,
			"name":       room.Name,
			"owner_id":   room.OwnerID,
			"role":       "owner",
			"created_at": room.CreatedAt.Format(time.RFC3339),
		},
		"file_id": fileID,
	})
}

func (h *Handler) ListRooms(c *gin.Context) {
	userID := c.GetString("userID")
	rooms, err := pg.ListRoomsForUser(c.Request.Context(), h.db, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list rooms"})
		return
	}
	result := make([]gin.H, 0, len(rooms))
	for _, r := range rooms {
		result = append(result, gin.H{
			"id":         r.ID,
			"name":       r.Name,
			"owner_id":   r.OwnerID,
			"role":       r.Role,
			"created_at": r.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"rooms": result})
}

func (h *Handler) GetRole(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"role": role})
}

func (h *Handler) GetFiles(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	files, err := pg.GetFilesForRoom(c.Request.Context(), h.db, roomID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get files"})
		return
	}
	result := make([]gin.H, 0, len(files))
	for _, f := range files {
		result = append(result, gin.H{
			"id":       f.ID,
			"name":     f.Name,
			"language": f.Language,
		})
	}
	c.JSON(http.StatusOK, gin.H{"files": result})
}

type updateFileRequest struct {
	Name     string `json:"name"     binding:"required,min=1,max=255"`
	Language string `json:"language" binding:"required"`
}

func (h *Handler) UpdateFile(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	var req updateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot rename files"})
		return
	}
	file, err := pg.UpdateFile(c.Request.Context(), h.db, fileID, req.Name, req.Language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update file"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"file": gin.H{
			"id":         file.ID,
			"name":       file.Name,
			"language":   file.Language,
			"updated_at": file.UpdatedAtStr(),
		},
	})
}

type addMemberRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role"  binding:"required,oneof=editor viewer"`
}

func (h *Handler) AddMember(c *gin.Context) {
	roomID := c.Param("roomID")
	callerID := c.GetString("userID")
	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, callerID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can invite members"})
		return
	}
	target, err := pg.GetUserByEmail(c.Request.Context(), h.db, strings.ToLower(req.Email))
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "no user found with that email"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if err := pg.AddMember(c.Request.Context(), h.db, roomID, target.ID, req.Role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not add member"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "member added",
		"user": gin.H{
			"id":    target.ID,
			"email": target.Email,
			"name":  target.Name,
		},
		"role": req.Role,
	})
}

// SaveState handles POST /api/rooms/:roomID/files/:fileID/state
// Body: raw binary Yjs encoded state (application/octet-stream)
// Only editors and owners can save.
func (h *Handler) SaveState(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")

	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot save"})
		return
	}

	state, err := io.ReadAll(c.Request.Body)
	if err != nil || len(state) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty state"})
		return
	}

	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.SaveDocument(c.Request.Context(), fileID, state); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save state"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "saved"})
}

// LoadState handles GET /api/rooms/:roomID/files/:fileID/state
// Returns raw Yjs binary state so the frontend can apply it before connecting WebSocket.
func (h *Handler) LoadState(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")

	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	doc, err := docRepo.LoadDocument(c.Request.Context(), fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load state"})
		return
	}

	if doc == nil || len(doc.YjsState) == 0 {
		c.Status(http.StatusNoContent)
		return
	}

	c.Data(http.StatusOK, "application/octet-stream", doc.YjsState)
}
