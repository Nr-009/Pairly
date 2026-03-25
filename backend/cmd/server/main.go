package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"go.uber.org/zap"

	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	"parily.dev/app/internal/health"
	"parily.dev/app/internal/logger"
	mongoClient "parily.dev/app/internal/mongo"
	mongoRepo "parily.dev/app/internal/mongo"
	"parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	"parily.dev/app/internal/rooms"
	wshandler "parily.dev/app/internal/websocket"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := logger.Init(cfg.Environment); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Log.Info("Parily server starting",
		zap.String("port", cfg.ServerPort),
		zap.String("environment", cfg.Environment),
	)

	pgPool, err := postgres.Connect(cfg)
	if err != nil {
		logger.Log.Fatal("Failed to connect to PostgreSQL", zap.Error(err))
	}
	defer pgPool.Close()
	logger.Log.Info("PostgreSQL connected")

	mongoDB, err := mongoClient.Connect(cfg)
	if err != nil {
		logger.Log.Fatal("Failed to connect to MongoDB", zap.Error(err))
	}
	logger.Log.Info("MongoDB connected", zap.String("db", mongoDB.Name()))

	redisClient, err := redis.Connect(cfg)
	if err != nil {
		logger.Log.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()
	logger.Log.Info("Redis connected")

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.PostgresUser,
		cfg.PostgresPassword,
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresDB,
	)

	m, err := migrate.New("file:///app/migrations", dsn)
	if err != nil {
		logger.Log.Fatal("Failed to initialize migrations", zap.Error(err))
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logger.Log.Fatal("Failed to run migrations", zap.Error(err))
	} else if err == migrate.ErrNoChange {
		logger.Log.Info("Migrations already up to date")
	} else {
		logger.Log.Info("Migrations applied successfully")
	}

	// ── Handlers ──────────────────────────────────────────────────────────────
	docRepo := mongoRepo.NewDocumentRepository(mongoDB)
	hub := wshandler.NewHub(redisClient, docRepo, logger.Log)
	wsHandler := wshandler.NewHandler(hub, pgPool, cfg, logger.Log)
	authHandler := auth.NewHandler(pgPool, cfg)
	roomsHandler := rooms.NewHandler(pgPool, mongoDB)

	// ── Router ────────────────────────────────────────────────────────────────
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))

	requireAuth := func(c *gin.Context) {
		claims, err := auth.ParseToken(c, cfg.JWTSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("userID", claims.UserID)
		c.Set("email", claims.Email)
	}

	r.GET("/health/live", health.Live)
	r.GET("/health/ready", health.Ready(pgPool, mongoDB, redisClient))

	authHandler.RegisterRoutes(r.Group("/auth"))

	r.GET("/auth/me", func(c *gin.Context) {
		requireAuth(c)
		if c.IsAborted() {
			return
		}
		authHandler.Me(c)
	})

	api := r.Group("/api")
	api.Use(func(c *gin.Context) {
		requireAuth(c)
		if c.IsAborted() {
			return
		}
		c.Next()
	})
	roomsHandler.RegisterRoutes(api.Group("/rooms"))

	// WebSocket — now includes fileId in the path
	r.GET("/ws/:roomId/:fileId", wsHandler.ServeWS)
	logger.Log.Info("Server listening", zap.String("port", cfg.ServerPort))
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		logger.Log.Fatal("Server failed", zap.Error(err))
	}
}
