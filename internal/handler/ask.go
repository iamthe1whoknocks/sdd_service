package handler

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"vulnrag/internal/rag"

	"vulnrag/internal/repository"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// AskHandler handles POST /api/v1/ask requests.
type AskHandler struct {
	Engine *rag.Engine
}

// PostApiV1Ask implements the ask endpoint.
func (h *AskHandler) PostApiV1Ask(c echo.Context) error {
	var req AskRequest
	if err := c.Bind(&req); err != nil {
		ErrResponse(http.StatusBadRequest,"invalid_request","malformed JSON request body")
	}

	// Validate question
	question := strings.TrimSpace(req.Question)
	if len(question) == 0 {
		ErrResponse(http.StatusUnprocessableEntity,"validation_error","question is required")
	}
	if utf8.RuneCountInString(question) > 4000 {
		ErrResponse(http.StatusUnprocessableEntity,"validation_error","question exceeds 4000 characters")
	}

	// Call RAG engine
	answer, sources, err := h.Engine.Ask(c.Request().Context(), question)
	if err != nil {
		if strings.Contains(err.Error(), "context deadline exceeded") {
			ErrResponse(http.StatusGatewayTimeout,"timeout","generation timed out after 60 seconds")
		}
		ErrResponse(http.StatusInternalServerError,"internal_error",err.Error())
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
	Engine *rag.Engine
}

// PostApiV1Documents implements the documents endpoint.
func (h *DocumentsHandler) PostApiV1Documents(c echo.Context) error {
	var req DocumentRequest
	if err := c.Bind(&req); err != nil {
		ErrResponse(http.StatusBadRequest,"invalid_request","malformed JSON request body")
	}

	// Validate title
	title := strings.TrimSpace(req.Title)
	if len(title) == 0 {
			ErrResponse(http.StatusUnprocessableEntity,"validation_error","title is required")
	}

	// Validate title
	err := repository.ValidateTitle(req.Title)
	if err !=nil {
		ErrResponse(http.StatusUnprocessableEntity,"validation_error","title exceeds 500 characters")
	}

	// Validate content size
	err = repository.ValidateQuestion(req.Content)
	if err !=nil {
		ErrResponse(http.StatusRequestEntityTooLarge,"payload_too_large","content exceeds 1 MB limit")
	}
	if len(strings.TrimSpace(req.Content)) == 0 {
		ErrResponse(http.StatusUnprocessableEntity,"validation_error","content is required")
	}

	// Index the document
	docID, err := h.Engine.IndexDocument(c.Request().Context(), req.Title, req.Content)
	if err != nil {
		ErrResponse(http.StatusUnprocessableEntity,"internal_error",err.Error())
	}

	// Parse docID as UUID
	docUUID, err := uuid.Parse(docID)
	if err != nil {
				ErrResponse(http.StatusInternalServerError,"internal_error","invalid document ID")

	}

	resp := DocumentResponse{
		DocumentId: openapi_types.UUID(docUUID),
		Indexed:    true,
	}

	return c.JSON(http.StatusOK, resp)
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}
