package vulnlist

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	vulnlistURL   = "https://bdu.fstec.ru/files/documents/vullist.xlsx"
	defaultSheet  = "Уязвимости"
	timeout       = 120          // seconds for download
	maxBodySize   = 50 << 20     // 50 MB - maximum allowed size for the vullist Excel file
)

// embeddedCACerts holds the official Russian Trusted CA certificates
// (Ministry of Digital Development and Communications) that sign the
// bdu.fstec.ru server certificate. They are embedded in the binary because
// the issuer is not part of the default system CA bundle (Mozilla) shipped
// with most operating systems.
//
//go:embed certs/*.crt
var embeddedCACerts embed.FS

// caPool builds the TLS root pool used to verify connections to
// bdu.fstec.ru: the system CA pool (Mozilla) extended with the embedded
// Russian Trusted Root CA and Russian Trusted Sub CA (2024).
func caPool() (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		// Fall back to an empty pool: the embedded certificates alone
		// are sufficient to verify bdu.fstec.ru.
		pool = x509.NewCertPool()
	}

	entries, err := embeddedCACerts.ReadDir("certs")
	if err != nil {
		return nil, fmt.Errorf("read embedded CA certs: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := "certs/" + entry.Name()
		pem, err := embeddedCACerts.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read embedded CA cert %s: %w", name, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("embedded CA cert %s contains no valid PEM block", name)
		}
	}
	return pool, nil
}

// Download fetches the vullist Excel file from the official FSTEC BDU registry.
// TLS is fully verified: the client trusts the system CA pool extended with
// the embedded Russian Trusted Root CA and Russian Trusted Sub CA (2024),
// which issue the bdu.fstec.ru server certificate.

// Row represents a single row from the vullist Excel file.
type Row struct {
	Headers []string
	Values  []string
}

func Download(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, vulnlistURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	pool, err := caPool()
	if err != nil {
		return nil, fmt.Errorf("build CA pool: %w", err)
	}

	client := &http.Client{
		Timeout: timeout * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Limit the response body size to prevent DoS via large files.
	resp.Body = http.MaxBytesReader(nil, resp.Body, maxBodySize)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}


// Parse reads the Excel file bytes and returns all rows with headers.
func Parse(data []byte) ([]Row, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open excel: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets in workbook")
	}

	// Try default sheet name first, fallback to the first sheet
	sheetName := defaultSheet
	if _, err := f.GetSheetIndex(sheetName); err != nil {
		sheetName = sheets[0]
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("read rows from sheet %q: %w", sheetName, err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows in sheet %q", sheetName)
	}

	// First row is the header
	headers := normalizeHeaders(rows[0])
	var result []Row

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		// Skip empty rows
		if isEmptyRow(row) {
			continue
		}

		values := make([]string, len(headers))
		for j := range headers {
			if j < len(row) {
				values[j] = strings.TrimSpace(row[j])
			}
		}
		result = append(result, Row{
			Headers: headers,
			Values:  values,
		})
	}

	return result, nil
}

// ToDocumentContent converts a Row into the document content format:
// "header1 : value1\nheader2 : value2\n..."
func ToDocumentContent(r Row) string {
	var sb strings.Builder
	for i, h := range r.Headers {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(h)
		sb.WriteString(" : ")
		if i < len(r.Values) {
			sb.WriteString(r.Values[i])
		}
	}
	return sb.String()
}

// ToDocumentTitle extracts the identifier (first column) as the document title.
func ToDocumentTitle(r Row) string {
	if len(r.Values) > 0 {
		return r.Values[0]
	}
	return ""
}

// isEmptyRow checks if a row is entirely empty.
func isEmptyRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// normalizeHeaders trims whitespace and spaces from header names.
func normalizeHeaders(headers []string) []string {
	result := make([]string, len(headers))
	for i, header := range headers {
		result[i] = strings.TrimSpace(header)
	}
	return result
}
