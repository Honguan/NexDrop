package nativebridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientForwardsAllowedShare(t *testing.T) {
	const token = "01234567890123456789012345678901"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/share" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("request = %s, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request Request
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Fatal("invalid request body")
		}
		_ = json.NewEncoder(w).Encode(Response{ID: request.ID, OK: true})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(SharePayload{Kind: "PAGE", URL: "https://example.com"})
	response := client.Handle(context.Background(), Request{ID: "request-1", Type: "share", Payload: payload})
	if !response.OK || response.ID != "request-1" {
		t.Fatalf("response = %+v", response)
	}
}

func TestClientRejectsNonLoopbackBridge(t *testing.T) {
	if _, err := NewClient("http://localhost:38473", "01234567890123456789012345678901"); err == nil {
		t.Fatal("NewClient() error = nil")
	}
}

func TestRunReadsAndWritesNativeFrames(t *testing.T) {
	const token = "01234567890123456789012345678901"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request Request
		_ = json.NewDecoder(r.Body).Decode(&request)
		_ = json.NewEncoder(w).Encode(Response{ID: request.ID, OK: true, Status: json.RawMessage(`{"connected":true}`)})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(Request{ID: "status-1", Type: "status"})
	var input bytes.Buffer
	_ = binary.Write(&input, binary.LittleEndian, uint32(len(content)))
	_, _ = input.Write(content)
	var output bytes.Buffer
	if err := Run(context.Background(), &input, &output, client); err != nil {
		t.Fatal(err)
	}
	result, err := readFrame(&output)
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if json.Unmarshal(result, &response) != nil || !response.OK || response.ID != "status-1" {
		t.Fatalf("response = %s", result)
	}
	if remaining, _ := io.ReadAll(&output); len(remaining) != 0 {
		t.Fatalf("remaining output = %x", remaining)
	}
}
