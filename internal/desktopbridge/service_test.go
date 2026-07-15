package desktopbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nexdrop/internal/nativebridge"
)

type testQueue struct{ payloads []nativebridge.SharePayload }

func (queue *testQueue) Enqueue(_ context.Context, payload nativebridge.SharePayload) (string, error) {
	queue.payloads = append(queue.payloads, payload)
	return "queue-1", nil
}

type testStatus struct{}

func (testStatus) Status(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"connected":true,"nodeURL":"https://drop.example"}`), nil
}

func TestWebPairsOnceAndQueuesValidatedShare(t *testing.T) {
	queue := &testQueue{}
	service, err := New("https://drop.example", queue, testStatus{})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := service.BeginPairing()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	token := redeemPairing(t, server.URL, pairing, "https://drop.example", http.StatusOK)
	if token == "" {
		t.Fatal("pairing returned an empty token")
	}
	redeemPairing(t, server.URL, pairing, "https://drop.example", http.StatusGone)
	payload, _ := json.Marshal(nativebridge.SharePayload{Kind: "PAGE", URL: "https://example.com"})
	requestBody, _ := json.Marshal(nativebridge.Request{ID: "request-1", Type: "share", Payload: payload})
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/share", bytes.NewReader(requestBody))
	request.Header.Set("Origin", "https://drop.example")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || len(queue.payloads) != 1 || queue.payloads[0].URL != "https://example.com" {
		t.Fatalf("share status = %d, queue = %+v", response.StatusCode, queue.payloads)
	}
}

func TestBridgeRejectsWrongOriginAndArbitraryFilePath(t *testing.T) {
	queue := &testQueue{}
	service, _ := New("https://drop.example", queue, testStatus{})
	token, _ := service.IssueNativeToken()
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	body := []byte(`{"id":"request-1","type":"share","payload":{"kind":"PAGE","url":"https://example.com","filePath":"C:\\secret.txt"}}`)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/share", bytes.NewReader(body))
	request.Header.Set("Origin", "https://drop.example")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || len(queue.payloads) != 0 {
		t.Fatalf("file path status = %d, queue = %+v", response.StatusCode, queue.payloads)
	}
	wrongOrigin, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/status", bytes.NewReader([]byte(`{"id":"status-1","type":"status"}`)))
	wrongOrigin.Header.Set("Origin", "https://attacker.example")
	wrongOrigin.Header.Set("Authorization", "Bearer "+token)
	wrongResponse, err := http.DefaultClient.Do(wrongOrigin)
	if err != nil {
		t.Fatal(err)
	}
	wrongResponse.Body.Close()
	if wrongResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong origin status = %d", wrongResponse.StatusCode)
	}
}

func TestNativeMessagingClientUsesAuthenticatedLocalChannel(t *testing.T) {
	service, _ := New("https://drop.example", &testQueue{}, testStatus{})
	token, _ := service.IssueNativeToken()
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	client, err := nativebridge.NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	response := client.Handle(context.Background(), nativebridge.Request{ID: "status-1", Type: "status"})
	if !response.OK || response.ID != "status-1" {
		t.Fatalf("native response = %+v", response)
	}
}

func redeemPairing(t *testing.T, baseURL string, pairing Pairing, origin string, expectedStatus int) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"pairingId": pairing.ID, "code": pairing.Code})
	request, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/pair", bytes.NewReader(body))
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		t.Fatalf("pair status = %d, want %d", response.StatusCode, expectedStatus)
	}
	var result struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(response.Body).Decode(&result)
	return result.Token
}
