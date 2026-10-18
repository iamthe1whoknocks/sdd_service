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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"vulnrag/internal/handler"
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

	// Download and index the official vullist on startup
	if err := initVulnlist(context.Background(), engine, zapLogger); err != nil {
		zapLogger.Warn("vullist indexing failed, service will continue without it", zap.Error(err))
	}

	// Create Echo router
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Set custom HTTP error handler
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if he, ok := err.(*echo.HTTPError); ok {
			resp := map[string]interface{}{
				"error": map[string]interface{}{
					"type":   he.Message,
					"detail": he.Error(),
				},
			}
			c.JSON(he.Code, resp)
		} else {
			c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error": map[string]string{
					"type":   "internal_error",
					"detail": err.Error(),
				},
			})
		}
	}

	// Register global middleware
	e.Use(middleware.Logger(zapLogger))
	e.Use(middleware.Recover(zapLogger))
	e.Use(middleware.RequestID())

	// Health endpoint
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Ask endpoint with timeout middleware
	askHandler := &handler.AskHandler{Engine: engine}
	e.POST("/api/v1/ask", askHandler.PostApiV1Ask)

	// Documents endpoint
	docHandler := &handler.DocumentsHandler{Engine: engine}
	e.POST("/api/v1/documents", docHandler.PostApiV1Documents)

	// Start server
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           e,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
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
