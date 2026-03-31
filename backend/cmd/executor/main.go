package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"parily.dev/app/internal/config"
	executor "parily.dev/app/internal/executor"
	"parily.dev/app/internal/kafka"
	"parily.dev/app/internal/metrics"
	mongoClient "parily.dev/app/internal/mongo"
	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
	"parily.dev/app/internal/tracing"
	pb "parily.dev/app/proto"
)

type executorServer struct {
	pb.UnimplementedExecutorServiceServer
	db        *pgxpool.Pool
	mongoDB   *mongo.Database
	dockerCli *dockerclient.Client
	rdb       *redis.Client
	kafka     *kafka.Producer
}

func lockKey(roomID, fileID string) string {
	return fmt.Sprintf("exec:lock:%s:%s", roomID, fileID)
}

func roomChannel(roomID string) string {
	return fmt.Sprintf("room:%s:room", roomID)
}

func (s *executorServer) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	log.Printf("Execute called: execution_id=%s room_id=%s file_id=%s",
		req.ExecutionId, req.RoomId, req.FileId)

	// try to acquire lock — 30s TTL matches execution timeout
	acquired, err := s.rdb.SetNX(lockKey(req.RoomId, req.FileId), "1", 30)
	if err != nil {
		return nil, fmt.Errorf("lock check failed: %w", err)
	}
	if !acquired {
		log.Printf("[main] lock held for room=%s file=%s", req.RoomId, req.FileId)
		return nil, status.Error(codes.ResourceExhausted, "already running")
	}

	log.Printf("[main] lock acquired for room=%s file=%s", req.RoomId, req.FileId)

	// pass ctx so the goroutine continues the same trace started in the WS server
	// the otelgrpc interceptor already injected the trace ID into this ctx
	spanCtx := oteltrace.SpanFromContext(ctx).SpanContext()
	execCtx := oteltrace.ContextWithSpanContext(context.Background(), spanCtx)
	go s.runExecution(execCtx, req.RoomId, req.FileId, req.ExecutionId)
	return &pb.ExecuteResponse{}, nil
}

func (s *executorServer) runExecution(ctx context.Context, roomID, fileID, executionID string) {
	log.Printf("[main] starting execution room=%s file=%s", roomID, fileID)

	// start a span that continues the trace from the WS server
	// this span will appear as a child of handleRunFile in Jaeger
	tracer := otel.Tracer("pairly")
	ctx, span := tracer.Start(ctx, "runExecution",
		oteltrace.WithAttributes(
			attribute.String("room.id", roomID),
			attribute.String("file.id", fileID),
			attribute.String("execution.id", executionID),
		),
	)
	defer span.End()

	defer func() {
		// always release lock after publishing done
		if err := s.rdb.Del(lockKey(roomID, fileID)); err != nil {
			log.Printf("[main] failed to delete lock: %v", err)
		}
		log.Printf("[main] lock released room=%s file=%s", roomID, fileID)
	}()

	// reconstruct file tree — ctx carries the trace so child spans attach correctly
	tempDir, entryPath, language, err := executor.ReconstructFileTree(ctx, s.db, s.mongoDB, roomID, executionID, fileID)
	if err != nil {
		log.Printf("[main] failed to reconstruct file tree: %v", err)
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		s.publishError(roomID, fileID, "internal_error")
		return
	}
	defer os.RemoveAll(tempDir)

	// run container — ctx carries the trace
	result, err := executor.RunContainer(ctx, s.dockerCli, tempDir, entryPath, language)
	if err != nil {
		log.Printf("[main] failed to run container: %v", err)
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		s.publishError(roomID, fileID, "internal_error")
		return
	}

	log.Printf("[main] execution done exit_code=%d duration=%dms", result.ExitCode, result.DurationMs)
	metrics.ExecutionsTotal.WithLabelValues(language).Inc()
	metrics.ExecutionDuration.Observe(float64(result.DurationMs) / 1000)

	// add execution result as span attributes for easy filtering in Jaeger
	span.SetAttributes(
		attribute.String("language", language),
		attribute.Int("exit.code", result.ExitCode),
		attribute.Int64("duration.ms", result.DurationMs),
	)

	// publish execution_done to Redis — RoomHub delivers to all clients
	event := map[string]any{
		"type":        "execution_done",
		"file_id":     fileID,
		"room_id":     roomID,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMs,
		"output":      result.Output,
		"truncated":   len(result.Output) >= 50*1024,
	}
	data, _ := json.Marshal(event)
	if err := s.rdb.Publish(roomChannel(roomID), data); err != nil {
		log.Printf("[main] failed to publish execution_done: %v", err)
		metrics.RedisPublishErrorsTotal.Inc()
	}
	log.Printf("[main] published execution_done to %s", roomChannel(roomID))

	// save to MongoDB
	execRepo := mongoRepo.NewExecutionRepository(s.mongoDB)
	if err := execRepo.SaveExecution(ctx, mongoRepo.ExecutionResult{
		ExecutionID: executionID,
		RoomID:      roomID,
		FileID:      fileID,
		Output:      result.Output,
		ExitCode:    result.ExitCode,
		DurationMs:  result.DurationMs,
		Truncated:   len(result.Output) >= 50*1024,
		ExecutedAt:  time.Now(),
	}); err != nil {
		log.Printf("[main] failed to save execution: %v", err)
	}

	// publish to Kafka async — audit log, nobody is waiting on this
	go func() {
		kctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		kafkaEvent := kafka.ExecutionEvent{
			ExecutionID: executionID,
			RoomID:      roomID,
			FileID:      fileID,
			Language:    language,
			Output:      result.Output,
			ExitCode:    result.ExitCode,
			DurationMs:  result.DurationMs,
			ExecutedAt:  time.Now().UTC(),
		}
		if err := s.kafka.PublishExecutionEvent(kctx, kafkaEvent); err != nil {
			log.Printf("[main] failed to publish execution event to kafka: %v", err)
			payload, _ := json.Marshal(kafkaEvent)
			_ = s.kafka.PublishDeadLetter(kctx, "execution-events", payload, err.Error())
			metrics.KafkaPublishErrorsTotal.WithLabelValues("execution-events").Inc()
		} else {
			log.Printf("[main] execution event published to kafka file=%s", fileID)
		}
	}()
}

func (s *executorServer) publishError(roomID, fileID, reason string) {
	event := map[string]string{
		"type":    "execution_error",
		"file_id": fileID,
		"reason":  reason,
	}
	data, _ := json.Marshal(event)
	if err := s.rdb.Publish(roomChannel(roomID), data); err != nil {
		log.Printf("[main] failed to publish execution_error: %v", err)
	}
}

func pullImages(ctx context.Context, cli *dockerclient.Client) {
	images := []string{
		"python:3.11-alpine",
		"node:20-alpine",
		"golang:1.21-alpine",
		"eclipse-temurin:21-alpine",
	}
	for _, img := range images {
		log.Printf("[startup] pulling image %s...", img)
		reader, err := cli.ImagePull(ctx, img, image.PullOptions{})
		if err != nil {
			log.Printf("[startup] failed to pull %s: %v", img, err)
			continue
		}
		io.Copy(io.Discard, reader)
		reader.Close()
		log.Printf("[startup] pulled %s", img)
	}
}

func main() {
	cfg, err := config.LoadExecutor()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	metrics.InitExecutor()

	shutdown, err := tracing.Init("pairly-executor", cfg.JaegerEndpoint)
	if err != nil {
		log.Fatalf("failed to init tracing: %v", err)
	}
	defer shutdown()

	pgPool, err := pg.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pgPool.Close()
	log.Println("postgres connected")

	mongoDB, err := mongoClient.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to mongodb: %v", err)
	}
	log.Println("mongodb connected")

	redisClient, err := redis.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	defer redisClient.Close()
	log.Println("redis connected")

	kafkaProducer := kafka.NewProducer(cfg.KafkaBroker)
	defer kafkaProducer.Close()
	log.Printf("kafka producer connected broker=%s", cfg.KafkaBroker)

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("failed to create docker client: %v", err)
	}
	defer cli.Close()
	log.Println("docker client connected")

	// start a small HTTP server just for Prometheus scraping
	// port from config matches what prometheus.yml expects for the executor target
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("executor metrics server listening on :%s", cfg.MetricsPort)
		if err := http.ListenAndServe(":"+cfg.MetricsPort, mux); err != nil {
			log.Printf("[main] metrics server error: %v", err)
		}
	}()

	pullImages(context.Background(), cli)
	log.Println("all images ready")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterExecutorServiceServer(s, &executorServer{
		db:        pgPool,
		mongoDB:   mongoDB,
		dockerCli: cli,
		rdb:       redisClient,
		kafka:     kafkaProducer,
	})

	log.Println("executor gRPC server listening on :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}