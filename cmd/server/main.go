// Package main is the entry point of the VulnRAG HTTP API.
//
// The service is a RAG assistant for vulnerabilities built on top of:
// - Echo v4 for HTTP routing
// - PostgreSQL 16 + pgvector for storage and search
// - Ollama for embeddings and answer generation
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"vulnrag/internal/config"
	"vulnrag/internal/handler"
	"vulnrag/internal/logger"
	"vulnrag/internal/rag"
	"vulnrag/internal/repository"
	"vulnrag/internal/vulnlist"
)

func main() {
	// Initialize logger
	lvl, err := zapcore.ParseLevel(config.String("LOG_LEVEL", "info"))
	if err != nil {
		lvl = zapcore.InfoLevel
	}
	zapLogger, err := logger.New(lvl)
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()

	// Load configuration from environment
	port := config.String("PORT", "8080")
	dbURL := config.String("DATABASE_URL", "postgres://vulnrag:vulnrag@localhost:5432/vulnrag?sslmode=disable")

	// Initialize database connection pool
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		zapLogger.Fatal("unable to create connection pool", zap.Error(err))
	}
	defer pool.Close()

	// Wait for database to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		zapLogger.Fatal("unable to ping database", zap.Error(err))
	}
	zapLogger.Info("connected to database")

	// Initialize RAG components
	ollama := rag.NewOllamaClient()
	repo := repository.New(pool)
	engine := rag.NewEngine(repo, ollama)

	// Verify that the embedding model dimension matches the database schema.
	// This prevents silent failures when the configured model returns a
	// different vector size than vector(768) in the migrations.
	if err := verifyEmbeddingDimension(context.Background(), ollama, 768, zapLogger); err != nil {
		zapLogger.Fatal("embedding dimension mismatch", zap.Error(err))
	}

	// Build the router with a shared pipeline: global middleware + generated
	// route registration from api/openapi.yaml (docs/FIXES.md #19).
	api := &handler.Server{
		AskHandler:       handler.NewAskHandler(engine, zapLogger),
		DocumentsHandler: handler.NewDocumentsHandler(engine, zapLogger),
	}
	e := handler.NewRouter(api, handler.RouterConfig{
		AskTimeout: 60 * time.Second,
	}, zapLogger)

	// Start server. WriteTimeout is aligned with the 60s /ask deadline plus a
	// small margin for writing the 504 response, so the deadline handled by
	// middleware.Timeout always fires before the connection is cut
	// (docs/FIXES.md #4).
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           e,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      65 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Index vullist in the background so the server becomes ready immediately
	// (docs/FIXES.md #7).  A per-start timeout prevents a slow Ollama from
	// hanging the startup goroutine forever.
	go func() {
		initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer initCancel()
		if err := initVulnlist(initCtx, engine, zapLogger); err != nil {
			zapLogger.Warn("vullist indexing failed, service will continue without it", zap.Error(err))
		}
	}()

	go func() {
		zapLogger.Info("VulnRAG API listening", zap.String("port", port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zapLogger.Fatal("server error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	zapLogger.Info("shutting down, waiting for active requests...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		zapLogger.Error("shutdown error", zap.Error(err))
	}
	zapLogger.Info("server stopped")
}

// upsertOutcome names how much work a single UpsertDocumentWithChunks call
// reported for one registry row.  It is extracted from initVulnlist so the
// counting rules behind docs/TODO_SIGNIFICANT.md SIG-2 are unit-testable:
// cmd/server has no tests of its own, and a misclassified row is exactly the
// failure that makes the startup summary lie about skipped computation.
func upsertOutcome(res rag.UpsertResult) string {
	switch {
	case res.Unchanged:
		return "unchanged"
	case res.Created:
		return "indexed"
	default:
		return "updated"
	}
}

func initVulnlist(ctx context.Context, engine *rag.Engine, logger *zap.Logger) error {
	logger.Info("downloading vullist from official source")

	// Step 1: Download the Excel file
	data, err := vulnlist.Download(ctx)
	if err != nil {
		return fmt.Errorf("download vullist: %w", err)
	}
	logger.Info("vullist downloaded", zap.Int("size_bytes", len(data)))

	// Step 2: Parse the Excel file
	rows, err := vulnlist.Parse(data)
	if err != nil {
		return fmt.Errorf("parse vullist: %w", err)
	}
	logger.Info("vullist parsed", zap.Int("rows", len(rows)))

	if len(rows) == 0 {
		return fmt.Errorf("vullist contains no data rows")
	}

	// Step 3: Upsert each row as a document (idempotent — no duplicates on
	// repeated starts, docs/FIXES.md #7).
	//
	// Counters are keyed by upsertOutcome so the summary reflects the actual
	// amount of work (docs/TODO_SIGNIFICANT.md SIG-2):
	//   indexed   — new document, embeddings generated and inserted
	//   updated   — existing document with changed content, re-embedded
	//   unchanged — existing document with identical content, Ollama untouched
	//   skipped   — rows that were not indexed at all (empty title, error)
	counts := map[string]int{"indexed": 0, "updated": 0, "unchanged": 0, "skipped": 0}
	for i, row := range rows {
		title := vulnlist.ToDocumentTitle(row)
		if title == "" {
			logger.Debug("skipping row with empty title", zap.Int("row", i+1))
			counts["skipped"]++
			continue
		}

		content := vulnlist.ToDocumentContent(row)
		res, err := engine.UpsertDocumentWithChunks(ctx, title, content)
		if err != nil {
			logger.Error("upserting row failed",
				zap.Int("row", i+1),
				zap.String("title", title),
				zap.Error(err),
			)
			counts["skipped"]++
			continue
		}

		outcome := upsertOutcome(res)
		counts[outcome]++
		switch outcome {
		case "unchanged":
			logger.Debug("skipped (content unchanged, embeddings not recomputed)",
				zap.Int("row", i+1),
				zap.String("title", title),
				zap.String("doc_id", res.DocID),
			)
		case "updated":
			logger.Debug("re-indexed (content changed)",
				zap.Int("row", i+1),
				zap.String("title", title),
				zap.String("doc_id", res.DocID),
			)
		}
	}

	logger.Info("vullist indexing completed",
		zap.Int("total", len(rows)),
		zap.Int("indexed", counts["indexed"]),
		zap.Int("updated", counts["updated"]),
		zap.Int("unchanged", counts["unchanged"]),
		zap.Int("skipped", counts["skipped"]),
	)

	return nil
}

// verifyEmbeddingDimension generates a test embedding and checks that its
// length matches the expected dimension.  This ensures the configured Ollama
// embedding model is compatible with the vector(N) column in the database
// (docs/FIXES.md #6).
func verifyEmbeddingDimension(ctx context.Context, ollama *rag.OllamaClient, expectedDim int, log *zap.Logger) error {
	log.Info("verifying embedding model dimension", zap.String("model", ollama.EmbedModel), zap.Int("expected_dim", expectedDim))

	// Use context with a short timeout so a hung Ollama call doesn't block startup.
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	emb, err := ollama.GenerateEmbedding(checkCtx, "dimension check probe")
	if err != nil {
		return fmt.Errorf("generate probe embedding: %w", err)
	}

	actualDim := len(emb)
	if actualDim != expectedDim {
		return fmt.Errorf("embedding dimension mismatch: model %q returned %d, expected %d — update vector(%d) in migrations or change EMBED_MODEL",
			ollama.EmbedModel, actualDim, expectedDim, actualDim)
	}

	log.Info("embedding dimension verified", zap.Int("dimension", actualDim))
	return nil
}
