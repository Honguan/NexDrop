package logging

import "context"

type requestIDKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func CorrelationAttributes(ctx context.Context, transferID string) []any {
	attributes := make([]any, 0, 4)
	if requestID := RequestID(ctx); requestID != "" {
		attributes = append(attributes, "request_id", requestID)
	}
	if transferID != "" {
		attributes = append(attributes, "transfer_id", transferID)
	}
	return attributes
}
