// Package main — точка входа HTTP API сервиса VulnRAG.
//
// Сервис реализует RAG-ассистента по уязвимостям с использованием:
// - Echo v4 для HTTP-маршрутизации
// - PostgreSQL 16 + pgvector для хранения и поиска
// - Ollama для эмбеддингов и генерации ответов
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
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"vulnrag/internal/handler"
	"vulnrag/internal/httperr"
	"vulnrag/internal/logger"
	"vulnrag/internal/middleware"
	"vulnrag/internal/rag"
	"vulnrag/internal/repository"
	"vulnrag/internal/vulnlist"
)

func main() {
	// Initialize logger
	lvl, err := zapcore.ParseLevel(getEnvDefault("LOG_LEVEL", "info"))
	if err != nil {
		lvl = zapcore.InfoLevel
	}
	zapLogger, err := logger.New(lvl)
	if err != nil {
		panic(err)
	}
	defer zapLogger.Sync()

	// Load configuration from environment
	port := getEnvDefault("PORT", "8080")
	dbURL := getEnvDefault("DATABASE_URL", "postgres://vulnrag:vulnrag@localhost:5432/vulnrag?sslmode=disable")

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

	// Download and index the official vullist on startup
	if err := initVulnlist(context.Background(), engine, zapLogger); err != nil {
		zapLogger.Warn("vullist indexing failed, service will continue without it", zap.Error(err))
	}

	// Create Echo router
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Set custom HTTP error handler: the full error goes to the log only,
	// the client gets a generic message (docs/FIXES.md #2).
	e.HTTPErrorHandler = newErrorHandler(zapLogger)

	// Register global middleware
	e.Use(middleware.Logger(zapLogger))
	e.Use(middleware.Recover(zapLogger))
	e.Use(middleware.RequestID())

	// Limit request body size to protect against DoS (docs/spec.md §4,
	// docs/FIXES.md #5).  The largest allowed payload is a 1 MB document
	// content wrapped in JSON, so ~1 MB + 64 KB overhead is sufficient.
	e.Use(echoMiddleware.BodyLimit("1MB+64KB"))

	// Health endpoint
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Ask endpoint: enforce the 60s generation deadline from the spec
	// (docs/spec.md §4/§9, docs/FIXES.md #4). middleware.Timeout puts the
	// deadline on the request context; the handler maps
	// context.DeadlineExceeded to 504 via errors.Is.
	askHandler := handler.NewAskHandler(engine, zapLogger)
	e.POST("/api/v1/ask", askHandler.PostApiV1Ask, middleware.Timeout(60*time.Second))

	// Documents endpoint
	docHandler := handler.NewDocumentsHandler(engine, zapLogger)
	e.POST("/api/v1/documents", docHandler.PostApiV1Documents)

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

	// Step 3: Index each row as a document
	var indexed, skipped int
	for i, row := range rows {
		title := vulnlist.ToDocumentTitle(row)
		if title == "" {
			logger.Debug("skipping row with empty title", zap.Int("row", i+1))
			skipped++
			continue
		}

		content := vulnlist.ToDocumentContent(row)
		if _, err := engine.IndexDocument(ctx, title, content); err != nil {
			logger.Error("indexing row failed",
				zap.Int("row", i+1),
				zap.String("title", title),
				zap.Error(err),
			)
			skipped++
			continue
		}
		indexed++
	}

	logger.Info("vullist indexing completed",
		zap.Int("total", len(rows)),
		zap.Int("indexed", indexed),
		zap.Int("skipped", skipped),
	)

	return nil
}

func getEnvDefault(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
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

// newErrorHandler returns the Echo HTTP error handler that logs the full
// error and replies with only generic details (docs/FIXES.md #2).
func newErrorHandler(log *zap.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		logErr := func(lvl zapcore.Level, msg string, status int) {
			if ce := log.Check(lvl, msg); ce != nil {
				ce.Write(
					zap.String("request_id", requestIDFrom(c)),
					zap.String("method", c.Request().Method),
					zap.String("path", c.Request().URL.Path),
					zap.Int("status", status),
					zap.Error(err),
				)
			}
		}

		he, ok := err.(*echo.HTTPError)
		if !ok {
			logErr(zapcore.ErrorLevel, "unhandled handler error", http.StatusInternalServerError)
			_ = httperr.Write(c, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}

		if he.Code >= http.StatusInternalServerError {
			logErr(zapcore.ErrorLevel, "handler error", he.Code)
			_ = httperr.Write(c, he.Code, "internal_error", "internal server error")
			return
		}

		// 4xx: the message is safe to expose (e.g. echo.ErrNotFound -> "Not Found").
		logErr(zapcore.InfoLevel, "request rejected", he.Code)
		detail := http.StatusText(he.Code)
		if msg, ok := he.Message.(string); ok && msg != "" {
			detail = msg
		}
		_ = httperr.Write(c, he.Code, "http_error", detail)
	}
}

// requestIDFrom returns the request ID set by the RequestID middleware.
func requestIDFrom(c echo.Context) string {
	if v, ok := c.Get("requestID").(string); ok {
		return v
	}
	return ""
}
