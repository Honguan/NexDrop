package monitoring

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type MetricKind string

const (
	MetricCounter MetricKind = "counter"
	MetricSummary MetricKind = "summary"
	MetricGauge   MetricKind = "gauge"
)

type MetricDefinition struct {
	Name   string
	Kind   MetricKind
	Labels []string
}

var OperationalMetrics = []MetricDefinition{
	{Name: "nexdrop_api_requests_total", Kind: MetricCounter, Labels: []string{"operation", "result", "error_code"}},
	{Name: "nexdrop_api_request_seconds", Kind: MetricSummary, Labels: []string{"operation", "result"}},
	{Name: "nexdrop_route_selection_total", Kind: MetricCounter, Labels: []string{"route", "result"}},
	{Name: "nexdrop_direct_handshake_seconds", Kind: MetricSummary, Labels: []string{"result", "error_code"}},
	{Name: "nexdrop_websocket_connection_total", Kind: MetricCounter, Labels: []string{"result"}},
	{Name: "nexdrop_websocket_heartbeat_gap_total", Kind: MetricCounter, Labels: []string{"result"}},
	{Name: "nexdrop_chunk_retry_total", Kind: MetricCounter, Labels: []string{"route", "result", "error_code"}},
	{Name: "nexdrop_checksum_failure_total", Kind: MetricCounter, Labels: []string{"operation", "result"}},
	{Name: "nexdrop_storage_rejection_total", Kind: MetricCounter, Labels: []string{"error_code", "result"}},
	{Name: "nexdrop_transfer_delivery_seconds", Kind: MetricSummary, Labels: []string{"route", "result"}},
	{Name: "nexdrop_transfer_throughput_bytes_per_second", Kind: MetricSummary, Labels: []string{"route"}},
	{Name: "nexdrop_database_operation_seconds", Kind: MetricSummary, Labels: []string{"operation", "result"}},
	{Name: "nexdrop_storage_operation_seconds", Kind: MetricSummary, Labels: []string{"operation", "result"}},
	{Name: "nexdrop_worker_recovery_total", Kind: MetricCounter, Labels: []string{"worker", "result"}},
	{Name: "nexdrop_worker_runs_total", Kind: MetricCounter, Labels: []string{"worker", "result"}},
	{Name: "nexdrop_transfer_state_total", Kind: MetricCounter, Labels: []string{"status"}},
	{Name: "nexdrop_transfer_queue_current", Kind: MetricGauge, Labels: []string{"status"}},
	{Name: "nexdrop_transfer_stalled_current", Kind: MetricGauge, Labels: []string{"status"}},
	{Name: "nexdrop_transfer_unacknowledged_current", Kind: MetricGauge, Labels: []string{"status"}},
	{Name: "nexdrop_dead_letter_current", Kind: MetricGauge, Labels: []string{"worker"}},
}

var boundedMetricValues = map[string]map[string]struct{}{
	"route": values("NONE", "LAN", "NODE", "MIXED", "WAITING_LAN", "LOCAL_DRAFT"),
	"status": values(
		"CREATED", "CHECKING_ROUTE", "WAITING_FOR_TARGET", "WAITING_FOR_NODE", "WAITING_FOR_LAN",
		"QUEUED", "UPLOADING_TO_NODE", "AVAILABLE_ON_NODE", "DOWNLOADING_FROM_NODE",
		"TRANSFERRING_LAN", "PAUSED", "VERIFYING", "DELIVERED", "READ", "FAILED",
		"CANCELLED", "EXPIRED", "SOURCE_FILE_MISSING", "SOURCE_FILE_CHANGED", "UNKNOWN",
	),
	"error_code": values(
		"NONE", "OTHER", "HASH_MISMATCH", "CHECKSUM_MISMATCH", "STORAGE_FULL", "DATABASE_UNAVAILABLE",
		"NETWORK_UNAVAILABLE", "TLS_AUTH_FAILED", "CONNECTION_RESET", "TIMEOUT",
		"SOURCE_FILE_MISSING", "SOURCE_FILE_CHANGED", "QUOTA_EXCEEDED",
	),
	"worker":    values("cleanup", "transfer", "analytics", "recovery"),
	"operation": values("request", "handshake", "heartbeat", "upload", "download", "complete", "query", "lock", "write", "cleanup", "recovery", "delivery"),
	"result":    values("success", "failure", "replay", "conflict", "connected", "replaced", "disconnected", "retry", "rejected"),
}

func values(items ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item] = struct{}{}
	}
	return result
}

func ValidateMetricLabels(labels []string) error {
	for _, label := range labels {
		if _, ok := boundedMetricValues[strings.ToLower(strings.TrimSpace(label))]; !ok {
			return fmt.Errorf("metric label %q is not bounded", label)
		}
	}
	return nil
}

func normalizeMetricLabels(labels map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(labels))
	for label, value := range labels {
		label = strings.ToLower(strings.TrimSpace(label))
		allowed, ok := boundedMetricValues[label]
		if !ok {
			return nil, fmt.Errorf("metric label %q is not bounded", label)
		}
		value = strings.TrimSpace(value)
		if _, ok := allowed[value]; !ok {
			if label == "error_code" {
				value = "OTHER"
			} else if label == "status" {
				value = "UNKNOWN"
			} else {
				return nil, fmt.Errorf("metric label %q value %q is not bounded", label, value)
			}
		}
		result[label] = value
	}
	return result, nil
}

type metricSample struct {
	definition MetricDefinition
	labels     map[string]string
	count      float64
	sum        float64
}

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]MetricDefinition
	samples     map[string]*metricSample
}

func NewRegistry() *Registry {
	definitions := make(map[string]MetricDefinition, len(OperationalMetrics))
	for _, definition := range OperationalMetrics {
		definitions[definition.Name] = definition
	}
	return &Registry{definitions: definitions, samples: make(map[string]*metricSample)}
}

var DefaultRegistry = NewRegistry()

func (registry *Registry) Add(name string, value float64, labels map[string]string) error {
	return registry.record(name, value, labels)
}

func (registry *Registry) Observe(name string, value float64, labels map[string]string) error {
	return registry.record(name, value, labels)
}

func (registry *Registry) Set(name string, value float64, labels map[string]string) error {
	definition, normalized, key, err := registry.prepare(name, labels)
	if err != nil {
		return err
	}
	if definition.Kind != MetricGauge {
		return fmt.Errorf("metric %q is not a gauge", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.samples[key] = &metricSample{definition: definition, labels: normalized, count: 1, sum: value}
	return nil
}

func (registry *Registry) record(name string, value float64, labels map[string]string) error {
	definition, normalized, key, err := registry.prepare(name, labels)
	if err != nil {
		return err
	}
	if definition.Kind == MetricGauge {
		return fmt.Errorf("metric %q requires Set", name)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	sample := registry.samples[key]
	if sample == nil {
		sample = &metricSample{definition: definition, labels: normalized}
		registry.samples[key] = sample
	}
	sample.count++
	sample.sum += value
	return nil
}

func (registry *Registry) prepare(name string, labels map[string]string) (MetricDefinition, map[string]string, string, error) {
	definition, ok := registry.definitions[name]
	if !ok {
		return MetricDefinition{}, nil, "", fmt.Errorf("unknown metric %q", name)
	}
	normalized, err := normalizeMetricLabels(labels)
	if err != nil {
		return MetricDefinition{}, nil, "", err
	}
	if len(normalized) != len(definition.Labels) {
		return MetricDefinition{}, nil, "", fmt.Errorf("metric %q label count mismatch", name)
	}
	for _, label := range definition.Labels {
		if _, ok := normalized[label]; !ok {
			return MetricDefinition{}, nil, "", fmt.Errorf("metric %q missing label %q", name, label)
		}
	}
	return definition, normalized, metricKey(name, normalized), nil
}

func metricKey(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString(name)
	for _, key := range keys {
		builder.WriteByte('|')
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(labels[key])
	}
	return builder.String()
}

func (registry *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	registry.mu.RLock()
	type snapshotEntry struct {
		key    string
		sample metricSample
	}
	snapshot := make([]snapshotEntry, 0, len(registry.samples))
	for key, sample := range registry.samples {
		snapshot = append(snapshot, snapshotEntry{key: key, sample: *sample})
	}
	registry.mu.RUnlock()
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].key < snapshot[j].key })
	documented := make(map[string]bool)
	for _, entry := range snapshot {
		sample := entry.sample
		if !documented[sample.definition.Name] {
			_, _ = fmt.Fprintf(w, "# TYPE %s %s\n", sample.definition.Name, sample.definition.Kind)
			documented[sample.definition.Name] = true
		}
		labels := prometheusLabels(sample.labels)
		if sample.definition.Kind == MetricSummary {
			_, _ = fmt.Fprintf(w, "%s_count%s %s\n", sample.definition.Name, labels, strconv.FormatFloat(sample.count, 'f', -1, 64))
			_, _ = fmt.Fprintf(w, "%s_sum%s %s\n", sample.definition.Name, labels, strconv.FormatFloat(sample.sum, 'f', -1, 64))
			continue
		}
		_, _ = fmt.Fprintf(w, "%s%s %s\n", sample.definition.Name, labels, strconv.FormatFloat(sample.sum, 'f', -1, 64))
	}
}

func prometheusLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(key)
		builder.WriteString(`="`)
		builder.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(labels[key]))
		builder.WriteByte('"')
	}
	builder.WriteByte('}')
	return builder.String()
}

var _ http.Handler = (*Registry)(nil)
