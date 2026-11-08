package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.uber.org/zap"

	"vulnrag/internal/validate"
)

// AskHandler handles POST /api/v1/ask requests.
type AskHandler struct {
	Engine Asker
	Logger *zap.Logger
}

// NewAskHandler creates the POST /api/v1/ask handler.
// The logger is created in cmd/server/main.go and passed here so that every
// handler error is logged with full details (see docs/FIXES.md #2).
func NewAskHandler(engine Asker, logger *zap.Logger) *AskHandler {
	return &AskHandler{Engine: engine, Logger: logger}
}

// PostApiV1Ask implements the ask endpoint.
func (h *AskHandler) PostApiV1Ask(c echo.Context) error {
	var req AskRequest
	if err := c.Bind(&req); err != nil {
		h.Logger.Warn("ask: malformed request body",
			zap.String("request_id", requestID(c)),
			zap.Error(err),
		)
		return respondError(c, http.StatusBadRequest, "invalid_request", "malformed JSON request body")
	}

	// Validate question (single validation path, docs/FIXES.md #14).
	question := strings.TrimSpace(req.Question)
	if err := validate.Question(question); err != nil {
		return respondValidationError(c, err)
	}

	// Call RAG engine under the 60s deadline set by middleware.Timeout
	// (cmd/server/main.go). A deadline is detected through the error chain
	// with errors.Is, never by string matching (docs/FIXES.md #4).
	answer, sources, err := h.Engine.Ask(c.Request().Context(), question)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.Logger.Warn("ask: generation timed out",
				zap.String("request_id", requestID(c)),
				zap.Error(err),
			)
			return respondError(c, http.StatusGatewayTimeout, "timeout", "generation timed out after 60 seconds")
		}
		h.Logger.Error("ask: engine failed",
			zap.String("request_id", requestID(c)),
			zap.Error(err),
		)
		return respondError(c, http.StatusInternalServerError, "internal_error", "internal server error")
	}

	// Build response sources
	responseSources := make([]Source, 0, len(sources))
	for _, s := range sources {
		src := Source{
			Title:   &s.Title,
			Snippet: &s.Content,
		}
		responseSources = append(responseSources, src)
	}

	resp := AskResponse{
		Answer:  answer,
		Sources: responseSources,
	}

	return c.JSON(http.StatusOK, resp)
}

// DocumentsHandler handles POST /api/v1/documents requests.
type DocumentsHandler struct {
	Engine Indexer
	Logger *zap.Logger
}

// NewDocumentsHandler creates the POST /api/v1/documents handler.
// The logger is created in cmd/server/main.go and passed here so that every
// handler error is logged with full details (see docs/FIXES.md #2).
func NewDocumentsHandler(engine Indexer, logger *zap.Logger) *DocumentsHandler {
	return &DocumentsHandler{Engine: engine, Logger: logger}
}

// PostApiV1Documents implements the documents endpoint.
func (h *DocumentsHandler) PostApiV1Documents(c echo.Context) error {
	var req DocumentRequest
	if err := c.Bind(&req); err != nil {
		h.Logger.Warn("documents: malformed request body",
			zap.String("request_id", requestID(c)),
			zap.Error(err),
		)
		return respondError(c, http.StatusBadRequest, "invalid_request", "malformed JSON request body")
	}

	// Validate payload: one path per field, all rules live in internal/validate
	// (docs/FIXES.md #14).
	title := strings.TrimSpace(req.Title)
	if err := validate.Title(title); err != nil {
		return respondValidationError(c, err)
	}
	if err := validate.Content(req.Content); err != nil {
		return respondValidationError(c, err)
	}

	// Index the document under the documents timeout set by middleware.Timeout.
	// Detect deadline via errors.Is (same pattern as AskHandler) so a timeout
	// returns 504 Gateway Timeout instead of 500 (docs/TODO_SIGNIFICANT.md SIG-4).
	docID, err := h.Engine.IndexDocument(c.Request().Context(), title, req.Content)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.Logger.Warn("documents: indexing timed out",
				zap.String("request_id", requestID(c)),
				zap.Error(err),
			)
			return respondError(c, http.StatusGatewayTimeout, "timeout", "document indexing timed out")
		}
		h.Logger.Error("documents: indexing failed",
			zap.String("request_id", requestID(c)),
			zap.String("title", title),
			zap.Error(err),
		)
		// An indexing failure is an internal server error (Ollama/DB down),
		// not a client validation problem (docs/FIXES.md #3).
		return respondError(c, http.StatusInternalServerError, "internal_error", "document indexing failed")
	}

	// Parse docID as UUID
	docUUID, err := uuid.Parse(docID)
	if err != nil {
		h.Logger.Error("documents: engine returned invalid document ID",
			zap.String("request_id", requestID(c)),
			zap.String("document_id", docID),
			zap.Error(err),
		)
		return respondError(c, http.StatusInternalServerError, "internal_error", "invalid document ID")
	}

	resp := DocumentResponse{
		DocumentId: openapi_types.UUID(docUUID),
		Indexed:    true,
	}

	return c.JSON(http.StatusOK, resp)
}
