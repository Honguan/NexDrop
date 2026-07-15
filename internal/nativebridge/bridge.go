package nativebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaximumMessageSize = 1 << 20

var ErrInvalidMessage = errors.New("invalid native message")

type Request struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Status json.RawMessage `json:"status,omitempty"`
}

type SharePayload struct {
	Kind            string   `json:"kind"`
	Title           string   `json:"title,omitempty"`
	URL             string   `json:"url,omitempty"`
	Text            string   `json:"text,omitempty"`
	TargetDeviceIDs []string `json:"targetDeviceIds,omitempty"`
	GroupID         string   `json:"groupId,omitempty"`
}

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || len(token) < 32 {
		return nil, ErrInvalidMessage
	}
	return &Client{baseURL: parsed, token: token, http: &http.Client{Timeout: 5 * time.Second}}, nil
}

func (client *Client) Handle(ctx context.Context, request Request) Response {
	if err := validateRequest(request); err != nil {
		return Response{ID: request.ID, Error: "INVALID_REQUEST"}
	}
	path := "/v1/status"
	if request.Type == "share" {
		path = "/v1/share"
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Response{ID: request.ID, Error: "INVALID_REQUEST"}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL.String()+path, bytes.NewReader(body))
	if err != nil {
		return Response{ID: request.ID, Error: "DESKTOP_UNAVAILABLE"}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+client.token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(httpRequest)
	if err != nil {
		return Response{ID: request.ID, Error: "DESKTOP_UNAVAILABLE"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Response{ID: request.ID, Error: "DESKTOP_REJECTED"}
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, MaximumMessageSize+1))
	if err != nil || len(content) > MaximumMessageSize {
		return Response{ID: request.ID, Error: "DESKTOP_INVALID_RESPONSE"}
	}
	var result Response
	if json.Unmarshal(content, &result) != nil || result.ID != request.ID {
		return Response{ID: request.ID, Error: "DESKTOP_INVALID_RESPONSE"}
	}
	return result
}

func validateRequest(request Request) error {
	if request.ID == "" || len(request.ID) > 100 {
		return ErrInvalidMessage
	}
	switch request.Type {
	case "status":
		if len(request.Payload) != 0 && string(request.Payload) != "null" {
			return ErrInvalidMessage
		}
		return nil
	case "share":
		var payload SharePayload
		if json.Unmarshal(request.Payload, &payload) != nil || !validShare(payload) {
			return ErrInvalidMessage
		}
		return nil
	default:
		return ErrInvalidMessage
	}
}

func validShare(payload SharePayload) bool {
	if len(payload.Title) > 1000 || len(payload.URL) > 8192 || len(payload.Text) > 100000 || len(payload.TargetDeviceIDs) > 100 || len(payload.GroupID) > 100 {
		return false
	}
	switch payload.Kind {
	case "PAGE", "LINK", "IMAGE":
		if payload.URL == "" {
			return false
		}
		parsed, err := url.Parse(payload.URL)
		return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
	case "SELECTION":
		return strings.TrimSpace(payload.Text) != ""
	default:
		return false
	}
}
