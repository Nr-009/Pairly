package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/grpc"

	"parily.dev/app/internal/config"
	executor "parily.dev/app/internal/executor"
	mongoClient "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
	pb "parily.dev/app/proto"
)

type executorServer struct {
	pb.UnimplementedExecutorServiceServer
	db        *pgxpool.Pool
	mongoDB   *mongo.Database
	dockerCli *dockerclient.Client
}

func (s *executorServer) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	log.Printf("Execute called: execution_id=%s room_id=%s file_id=%s",
		req.ExecutionId, req.RoomId, req.FileId)

	tempDir, entryPath, language, err := executor.ReconstructFileTree(ctx, s.db, s.mongoDB, req.RoomId, req.ExecutionId, req.FileId)
	if err != nil {
		log.Printf("[main] failed to reconstruct file tree: %v", err)
		return nil, fmt.Errorf("reconstruct file tree: %w", err)
	}
	log.Printf("[main] file tree ready tempDir=%s entry=%s language=%s", tempDir, entryPath, language)

	result, err := executor.RunContainer(s.dockerCli, tempDir, entryPath, language)
	if err != nil {
		log.Printf("[main] failed to run container: %v", err)
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("run container: %w", err)
	}

	os.RemoveAll(tempDir)
	log.Printf("[main] cleaned up %s", tempDir)
	log.Printf("[main] returning response exit_code=%d duration=%dms output_len=%d",
		result.ExitCode, result.DurationMs, len(result.Output))

	return &pb.ExecuteResponse{
		Output:     result.Output,
		ExitCode:   int32(result.ExitCode),
		DurationMs: result.DurationMs,
		Truncated:  false,
	}, nil
}

func pullImages(ctx context.Context, cli *dockerclient.Client) {
	images := []string{
		"python:3.11-alpine",
		"node:20-alpine",
		"golang:1.21-alpine",
		"openjdk:21-alpine",
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

	// create docker client once — reused for pulls and container runs
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("failed to create docker client: %v", err)
	}
	defer cli.Close()
	log.Println("docker client connected")

	// pull all images before accepting any requests
	pullImages(context.Background(), cli)
	log.Println("all images ready")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterExecutorServiceServer(s, &executorServer{
		db:        pgPool,
		mongoDB:   mongoDB,
		dockerCli: cli,
	})

	log.Println("executor gRPC server listening on :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}