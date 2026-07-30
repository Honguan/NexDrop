package monitoring

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type blockingWriter struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
}

func (writer *blockingWriter) Header() http.Header { return writer.header }
func (*blockingWriter) WriteHeader(int)            {}
func (writer *blockingWriter) Write(payload []byte) (int, error) {
	select {
	case writer.started <- struct{}{}:
	default:
	}
	<-writer.release
	return len(payload), nil
}

func TestMetricContractRejectsHighCardinalityLabels(t *testing.T) {
	for _, labels := range [][]string{
		{"transfer_id"},
		{"device_id"},
		{"user_id"},
		{"route", "error_code", "request_id"},
	} {
		if err := ValidateMetricLabels(labels); err == nil {
			t.Fatalf("ValidateMetricLabels(%v) accepted high-cardinality labels", labels)
		}
	}
	if err := ValidateMetricLabels([]string{"route", "status", "error_code"}); err != nil {
		t.Fatalf("bounded labels rejected: %v", err)
	}
	for _, metric := range OperationalMetrics {
		if err := ValidateMetricLabels(metric.Labels); err != nil {
			t.Fatalf("%s labels rejected: %v", metric.Name, err)
		}
	}
}

func TestRegistryExportsBoundedMetricsWithoutRawIdentifiers(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Add("nexdrop_api_requests_total", 1, map[string]string{
		"operation":  "request",
		"result":     "failure",
		"error_code": "DEVICE_019fc1a4",
	}); err != nil {
		t.Fatalf("record metric: %v", err)
	}
	if err := registry.Add("nexdrop_api_requests_total", 1, map[string]string{
		"operation":  "request",
		"result":     "success",
		"request_id": "raw-id",
	}); err == nil {
		t.Fatal("record accepted a high-cardinality label")
	}

	response := httptest.NewRecorder()
	registry.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	if !strings.Contains(body, `error_code="OTHER"`) {
		t.Fatalf("unknown error code was not normalized: %s", body)
	}
	for _, forbidden := range []string{"DEVICE_019fc1a4", "raw-id"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metric output leaked %q: %s", forbidden, body)
		}
	}
}

func TestRegistryExportsCurrentValueGauges(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Set("nexdrop_transfer_queue_current", 7, map[string]string{"status": "QUEUED"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add("nexdrop_transfer_queue_current", 1, map[string]string{"status": "QUEUED"}); err == nil {
		t.Fatal("gauge accepted Add")
	}
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(response.Body.String(), `nexdrop_transfer_queue_current{status="QUEUED"} 7`) {
		t.Fatalf("gauge output = %s", response.Body.String())
	}
}

func TestMetricsScrapeDoesNotHoldRegistryLockDuringNetworkWrite(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Add("nexdrop_api_requests_total", 1, map[string]string{
		"operation": "request", "result": "success", "error_code": "NONE",
	}); err != nil {
		t.Fatal(err)
	}
	writer := &blockingWriter{
		header: make(http.Header), started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	scrapeDone := make(chan struct{})
	go func() {
		registry.ServeHTTP(writer, httptest.NewRequest("GET", "/metrics", nil))
		close(scrapeDone)
	}()
	<-writer.started
	recorded := make(chan error, 1)
	go func() {
		recorded <- registry.Add("nexdrop_api_requests_total", 1, map[string]string{
			"operation": "request", "result": "success", "error_code": "NONE",
		})
	}()
	select {
	case err := <-recorded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("metric recording blocked behind a slow scrape")
	}
	close(writer.release)
	<-scrapeDone
}
