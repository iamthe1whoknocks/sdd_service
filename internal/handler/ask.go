package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"vulnrag/internal/rag"
)

// AskHandler handles POST /api/v1/ask requests.
type AskHandler struct {
	Engine *rag.Engine
}

// PostApiV1Ask implements the ask endpoint.
func (h *AskHandler) PostApiV1Ask(c echo.Context) error {
	var req AskRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("invalid_request"),
				Detail: strPtr("malformed JSON request body"),
			},
		})
	}

	// Validate question
	question := strings.TrimSpace(req.Question)
	if len(question) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("validation_error"),
				Detail: strPtr("question is required"),
			},
		})
	}
	if len(question) > 4000 {
		return c.JSON(http.StatusUnprocessableEntity, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("validation_error"),
				Detail: strPtr("question exceeds 4000 characters"),
			},
		})
	}

	// Call RAG engine
	result, sources, err := h.Engine.Ask(c.Request().Context(), question)
	if err != nil {
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return c.JSON(http.StatusGatewayTimeout, Error{
				Error: struct {
					Detail *string `json:"detail,omitempty"`
					Type   *string `json:"type,omitempty"`
				}{
					Type:   strPtr("timeout"),
					Detail: strPtr("generation timed out after 60 seconds"),
				},
			})
		}
		return c.JSON(http.StatusInternalServerError, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("internal_error"),
				Detail: strPtr(err.Error()),
			},
		})
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
		Answer:  result.Answer,
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
		return c.JSON(http.StatusBadRequest, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("invalid_request"),
				Detail: strPtr("malformed JSON request body"),
			},
		})
	}

	// Validate title
	title := strings.TrimSpace(req.Title)
	if len(title) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("validation_error"),
				Detail: strPtr("title is required"),
			},
		})
	}
	if len(req.Title) > 500 {
		return c.JSON(http.StatusUnprocessableEntity, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("validation_error"),
				Detail: strPtr("title exceeds 500 characters"),
			},
		})
	}

	// Validate content size
	if len(req.Content) > 1048576 {
		return c.JSON(http.StatusRequestEntityTooLarge, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("payload_too_large"),
				Detail: strPtr("content exceeds 1 MB limit"),
			},
		})
	}
	if len(strings.TrimSpace(req.Content)) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("validation_error"),
				Detail: strPtr("content is required"),
			},
		})
	}

	// Index the document
	docID, err := h.Engine.IndexDocument(c.Request().Context(), req.Title, req.Content)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("internal_error"),
				Detail: strPtr(err.Error()),
			},
		})
	}

	// Parse docID as UUID
	docUUID, err := uuid.Parse(docID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   strPtr("internal_error"),
				Detail: strPtr("invalid document ID"),
			},
		})
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
