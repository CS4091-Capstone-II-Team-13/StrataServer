package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"strata.themrdt.org/dev/server/internal/config"
	"strata.themrdt.org/dev/server/internal/handler"
	"strata.themrdt.org/dev/server/internal/middleware"
	"strata.themrdt.org/dev/server/internal/service"
	miniostore "strata.themrdt.org/dev/server/internal/store/minio"
	"strata.themrdt.org/dev/server/internal/store/postgres"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("strata: %v", err)
	}
}

func run() error {
	// ── Load config ──────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Connect to PostgreSQL ────────────────────────────────────
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("parse db config: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.Database.MaxConns)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	log.Println("strata: connected to PostgreSQL")

	// ── Connect to MinIO ─────────────────────────────────────────
	minioClient, err := minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})
	if err != nil {
		return fmt.Errorf("connect to minio: %w", err)
	}

	// Ensure the chunk bucket exists.
	err = minioClient.MakeBucket(ctx, cfg.MinIO.ChunkBucket, minio.MakeBucketOptions{})
	if err != nil {
		// Check if it already exists (not an error).
		exists, existsErr := minioClient.BucketExists(ctx, cfg.MinIO.ChunkBucket)
		if existsErr != nil || !exists {
			return fmt.Errorf("create minio bucket %q: %w", cfg.MinIO.ChunkBucket, err)
		}
	}
	log.Printf("strata: connected to MinIO (bucket: %s)", cfg.MinIO.ChunkBucket)

	// ── Initialize stores ────────────────────────────────────────
	userStore := postgres.NewUserStore(pool)
	projectStore := postgres.NewProjectStore(pool)
	chunkStore := postgres.NewChunkStore(pool)
	commitStore := postgres.NewCommitStore(pool)
	fileStore := postgres.NewFileStore(pool)
	lockStore := postgres.NewLockStore(pool)
	chunkBlobStore := miniostore.NewChunkStore(minioClient, cfg.MinIO.ChunkBucket)

	// ── Initialize services ──────────────────────────────────────
	authService := service.NewAuthService(userStore, cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL)
	projectService := service.NewProjectService(projectStore)
	chunkService := service.NewChunkService(chunkStore, chunkBlobStore, cfg.Chunks.MaxSize)
	versionService := service.NewVersionService(commitStore, fileStore, chunkStore)
	mergeService := service.NewMergeService(commitStore, fileStore, chunkStore)
	lockService := service.NewLockService(lockStore)

	// ── Initialize handlers ──────────────────────────────────────
	authHandler := handler.NewAuthHandler(authService)
	projectHandler := handler.NewProjectHandler(projectService)
	pushHandler := handler.NewPushHandler(chunkService, versionService, lockService)
	pullHandler := handler.NewPullHandler(chunkService, versionService)
	versionHandler := handler.NewVersionHandler(versionService)
	mergeHandler := handler.NewMergeHandler(mergeService, commitStore)
	lockHandler := handler.NewLockHandler(lockService)

	// ── Build router ─────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware.
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))

	// CORS — allow the web UI origin.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"https://strata.themrdt.org",
			"https://strata-web.themrdt.org",
			"http://localhost:5173",
			"http://localhost:3000",
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Chunk-Hash"},
		ExposedHeaders:   []string{"Content-Length", "X-Chunk-Hash"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health check — no auth required.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"strata"}`))
	})

	// API v1 routes.
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes — no auth.
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", authHandler.Register)
			r.Post("/login", authHandler.Login)
			r.Post("/refresh", authHandler.Refresh)
			r.Post("/logout", authHandler.Logout)
		})

		// Protected routes — require valid access token.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.Auth.JWTSecret))

			// Projects.
			r.Route("/projects", func(r chi.Router) {
				r.Post("/", projectHandler.Create)
				r.Get("/", projectHandler.List)

				r.Route("/{projectID}", func(r chi.Router) {
					r.Get("/", projectHandler.Get)
					r.Delete("/", projectHandler.Delete)

					// Push (client → server).
					r.Route("/push", func(r chi.Router) {
						r.Post("/check", pushHandler.CheckChunks)
						r.Post("/chunk", pushHandler.UploadChunk)
						r.Post("/commit", pushHandler.Commit)
					})

					// Pull (server → client).
					r.Route("/pull", func(r chi.Router) {
						r.Post("/diff", pullHandler.Diff)
						r.Get("/chunk/{hash}", pullHandler.DownloadChunk)
					})

					// Branching and merging.
					r.Post("/branches", mergeHandler.CreateBranch)
					r.Post("/merge/analyze", mergeHandler.AnalyzeMerge)
					r.Post("/merge", mergeHandler.ExecuteMerge)

					// File locks.
					r.Route("/locks", func(r chi.Router) {
						r.Get("/", lockHandler.List)
						r.Post("/", lockHandler.Acquire)
						r.Delete("/", lockHandler.Release)
					})

					// Version browsing.
					r.Get("/commits", versionHandler.ListCommits)
					r.Get("/commits/{commitID}", versionHandler.GetCommit)
					r.Get("/commits/{commitID}/files", versionHandler.ListCommitFiles)
					r.Get("/refs", versionHandler.ListRefs)
					r.Get("/tree", versionHandler.GetTree)
				})
			})
		})
	})

	// ── Start server ─────────────────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.Server.Addr(),
		Handler:      r,
		ReadTimeout:  5 * time.Minute, // Large chunk uploads need time.
		WriteTimeout: 5 * time.Minute, // Large chunk downloads need time.
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("strata: listening on %s", cfg.Server.Addr())
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Printf("strata: received signal %s, shutting down...", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	log.Println("strata: shutdown complete")
	return nil
}
