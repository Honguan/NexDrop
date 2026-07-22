package main

import (
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	values := []time.Duration{5 * time.Millisecond, time.Millisecond, 4 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}
	if got := percentile(values, 50); got != 3*time.Millisecond {
		t.Fatalf("p50 = %s", got)
	}
	if got := percentile(values, 95); got != 5*time.Millisecond {
		t.Fatalf("p95 = %s", got)
	}
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("empty p95 = %s", got)
	}
}

func TestValidateScenario(t *testing.T) {
	if err := validateScenario(100, 50, 10); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ devices, online, transfers int }{
		{0, 0, 0}, {10, 11, 1}, {10, 5, 11},
	} {
		if err := validateScenario(test.devices, test.online, test.transfers); err == nil {
			t.Fatalf("validateScenario(%d, %d, %d) succeeded", test.devices, test.online, test.transfers)
		}
	}
}

func TestActiveTransferStatus(t *testing.T) {
	for _, status := range []string{"CREATED", "QUEUED", "UPLOADING_TO_NODE", "WAITING_FOR_LAN", "VERIFYING"} {
		if !activeTransferStatus(status) {
			t.Fatalf("%s should be active", status)
		}
	}
	for _, status := range []string{"DELIVERED", "READ", "FAILED", "CANCELLED", "EXPIRED", "SOURCE_FILE_MISSING"} {
		if activeTransferStatus(status) {
			t.Fatalf("%s should not be active", status)
		}
	}
}

func TestLatencyTargetIsStrict(t *testing.T) {
	maximum := 500 * time.Millisecond
	if !meetsLatencyTarget(latencyResult{P95: maximum - time.Nanosecond}, maximum) {
		t.Fatal("value below maximum should pass")
	}
	if meetsLatencyTarget(latencyResult{P95: maximum}, maximum) {
		t.Fatal("value equal to maximum should fail")
	}
	if meetsLatencyTarget(latencyResult{P95: time.Millisecond, Failures: 1}, maximum) {
		t.Fatal("failed request should fail acceptance")
	}
}

func TestNodeKeyHeaders(t *testing.T) {
	if headers := nodeKeyHeaders("  "); headers != nil {
		t.Fatalf("empty node key headers = %#v", headers)
	}
	const nodeKey = "test-node-key"
	if got := nodeKeyHeaders(nodeKey)["X-NexDrop-Node-Key"]; got != nodeKey {
		t.Fatalf("node key header = %q", got)
	}
}
