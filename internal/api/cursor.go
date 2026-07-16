package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

func encodeCursor(key []byte, createdAt time.Time, id string) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	signature := hmac.New(sha256.New, key)
	_, _ = signature.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

func decodeCursor(key []byte, value string) (time.Time, string, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	suppliedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	expectedSignature := hmac.New(sha256.New, key)
	_, _ = expectedSignature.Write(payload)
	if !hmac.Equal(suppliedSignature, expectedSignature.Sum(nil)) {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	fields := strings.SplitN(string(payload), "|", 2)
	if len(fields) != 2 || !validUUID(fields[1]) {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return time.Time{}, "", errors.New("invalid cursor")
	}
	return createdAt.UTC(), fields[1], nil
}
