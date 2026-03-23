package health

import (
	"context"
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

func Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func Ready(pg *pgxpool.Pool, mdb *mongo.Database, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := pg.Ping(context.Background()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "postgres down", "error": err.Error()})
			return
		}
		if err := mdb.Client().Ping(context.Background(), nil); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "mongodb down", "error": err.Error()})
			return
		}
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis down", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
