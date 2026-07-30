package logging

import (
	"context"
	"testing"
)

func TestCorrelationAttributesIncludeRequestAndTransfer(t *testing.T) {
	attributes := CorrelationAttributes(WithRequestID(context.Background(), "request-1"), "transfer-1")
	if len(attributes) != 4 || attributes[1] != "request-1" || attributes[3] != "transfer-1" {
		t.Fatalf("attributes = %#v", attributes)
	}
}
