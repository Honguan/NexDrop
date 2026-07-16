package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type configuration struct {
	baseURL      string
	username     string
	password     string
	requests     int
	concurrency  int
	maximumP95   time.Duration
	setup        bool
	devices      int
	online       int
	transfers    int
	reportPath   string
	environment  string
	postgresInfo string
}

type latencyResult struct {
	Requests    int           `json:"requests"`
	Concurrency int           `json:"concurrency"`
	Failures    int64         `json:"failures"`
	SuccessRate float64       `json:"successRate"`
	P50         time.Duration `json:"-"`
	P95         time.Duration `json:"-"`
	P50Millis   float64       `json:"p50Millis"`
	P95Millis   float64       `json:"p95Millis"`
}

type scenarioResult struct {
	RegisteredDevices int `json:"registeredDevices"`
	OnlineDevices     int `json:"onlineDevices"`
	ActiveTransfers   int `json:"activeTransfers"`
}

type report struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Node        map[string]any `json:"node"`
	Environment string         `json:"environment"`
	PostgreSQL  string         `json:"postgresql"`
	Runtime     map[string]any `json:"runtime"`
	Scenario    scenarioResult `json:"scenario"`
	Latency     latencyResult  `json:"latency"`
	Acceptance  map[string]any `json:"acceptance"`
}

type apiClient struct {
	baseURL string
	http    *http.Client
}

type tokenPair struct {
	AccessToken string `json:"accessToken"`
}

type deviceResponse struct {
	ID string `json:"id"`
}

type loadDevice struct {
	id    string
	token string
}

type presenceConnection struct {
	connection *websocket.Conn
	cancel     context.CancelFunc
	alive      atomic.Bool
	mu         sync.Mutex
}

type scenarioState struct {
	deviceIDs   []string
	connections []*presenceConnection
	transferIDs []string
	token       string
}

type transferResponse struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	Status      string `json:"status"`
}

func main() {
	config := parseFlags()
	if config.requests < 1 || config.concurrency < 1 {
		exitUsage("requests and concurrency must be positive")
	}
	if config.setup {
		if config.username == "" || config.password == "" {
			exitUsage("username and password are required for scenario setup")
		}
		if err := validateScenario(config.devices, config.online, config.transfers); err != nil {
			exitUsage(err.Error())
		}
	}

	client := &apiClient{baseURL: strings.TrimRight(config.baseURL, "/"), http: &http.Client{Timeout: 10 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	node := map[string]any{}
	if err := client.request(ctx, http.MethodGet, "/api/version", "", nil, http.StatusOK, &node); err != nil {
		fatal(err)
	}
	scenario := scenarioResult{}
	state := scenarioState{}
	var scenarioError error
	if config.setup {
		var err error
		state, err = client.setupScenario(ctx, config)
		if err != nil {
			fatal(err)
		}
		defer func() {
			for _, connection := range state.connections {
				connection.close()
			}
		}()
	}

	latency := runLatency(ctx, config)
	if config.setup {
		scenario, scenarioError = client.verifyScenario(ctx, state)
	}
	capacityPassed := !config.setup || (scenario.RegisteredDevices >= config.devices && scenario.OnlineDevices >= config.online && scenario.ActiveTransfers >= config.transfers)
	latencyPassed := meetsLatencyTarget(latency, config.maximumP95)
	result := report{
		GeneratedAt: time.Now().UTC(), Node: node, Environment: config.environment, PostgreSQL: config.postgresInfo,
		Runtime:  map[string]any{"goVersion": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "logicalCPUs": runtime.NumCPU()},
		Scenario: scenario, Latency: latency,
		Acceptance: map[string]any{
			"requiredDevices": config.devices, "requiredOnline": config.online, "requiredTransfers": config.transfers,
			"maximumP95Millis": float64(config.maximumP95) / float64(time.Millisecond), "capacityPassed": capacityPassed,
			"latencyPassed": latencyPassed,
			"passed":        scenarioError == nil && capacityPassed && latencyPassed,
		},
	}
	if scenarioError != nil {
		result.Acceptance["scenarioError"] = scenarioError.Error()
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(encoded))
	if config.reportPath != "" {
		if err := os.WriteFile(config.reportPath, append(encoded, '\n'), 0o600); err != nil {
			fatal(err)
		}
	}
	if scenarioError != nil || !capacityPassed || !latencyPassed {
		os.Exit(1)
	}
}

func parseFlags() configuration {
	var config configuration
	flag.StringVar(&config.baseURL, "url", "http://127.0.0.1:8080", "NexDrop Node base URL")
	flag.StringVar(&config.username, "username", "", "scenario account username")
	flag.StringVar(&config.password, "password", "", "scenario account password")
	flag.IntVar(&config.requests, "requests", 1000, "number of requests")
	flag.IntVar(&config.concurrency, "concurrency", 10, "parallel requests")
	flag.DurationVar(&config.maximumP95, "max-p95", 500*time.Millisecond, "maximum accepted p95")
	flag.BoolVar(&config.setup, "setup-scenario", false, "create and hold the capacity acceptance scenario")
	flag.IntVar(&config.devices, "devices", 100, "registered devices in the scenario")
	flag.IntVar(&config.online, "online", 50, "simultaneously online devices in the scenario")
	flag.IntVar(&config.transfers, "transfers", 10, "simultaneously active transfers in the scenario")
	flag.StringVar(&config.reportPath, "report", "", "JSON report output path")
	flag.StringVar(&config.environment, "environment", "unspecified", "test environment description")
	flag.StringVar(&config.postgresInfo, "postgres", "unspecified", "PostgreSQL environment description")
	flag.Parse()
	return config
}

func validateScenario(devices, online, transfers int) error {
	if devices < 1 || online < 1 || transfers < 1 {
		return errors.New("scenario counts must be positive")
	}
	if online > devices {
		return errors.New("online devices cannot exceed registered devices")
	}
	if transfers > devices-1 {
		return errors.New("transfers require distinct target devices")
	}
	return nil
}

func (client *apiClient) setupScenario(ctx context.Context, config configuration) (scenarioState, error) {
	admin, err := client.login(ctx, config.username, config.password)
	if err != nil {
		return scenarioState{}, err
	}
	devices := make([]loadDevice, 0, config.devices)
	for index := 0; index < config.devices; index++ {
		pair, err := client.login(ctx, config.username, config.password)
		if err != nil {
			return scenarioState{}, fmt.Errorf("login device %d: %w", index+1, err)
		}
		privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return scenarioState{}, err
		}
		request := map[string]any{"displayName": fmt.Sprintf("Load device %03d", index+1), "type": "WEB_CHROME", "publicKey": privateKey.PublicKey().Bytes(), "keyAlgorithm": "X25519"}
		var created deviceResponse
		if err := client.request(ctx, http.MethodPost, "/api/devices", pair.AccessToken, request, http.StatusCreated, &created); err != nil {
			return scenarioState{}, fmt.Errorf("create device %d: %w", index+1, err)
		}
		devices = append(devices, loadDevice{id: created.ID, token: pair.AccessToken})
	}
	for index, device := range devices {
		if err := client.request(ctx, http.MethodPost, "/api/devices/"+device.id+"/approve", admin.AccessToken, nil, http.StatusOK, nil); err != nil {
			return scenarioState{}, fmt.Errorf("approve device %d: %w", index+1, err)
		}
	}

	connections := make([]*presenceConnection, 0, config.online)
	tokens := make([]string, 0, config.online)
	for index := 0; index < config.online; index++ {
		connection, err := client.connectPresence(ctx, devices[index].token)
		if err != nil {
			closeConnections(connections)
			return scenarioState{}, fmt.Errorf("connect online device %d: %w", index+1, err)
		}
		connections = append(connections, connection)
		tokens = append(tokens, devices[index].token)
	}

	transferIDs := make([]string, 0, config.transfers)
	for index := 0; index < config.transfers; index++ {
		target := devices[(index+1)%len(devices)].id
		wrappedKey := make([]byte, 32)
		_, _ = rand.Read(wrappedKey)
		request := map[string]any{
			"targetType": "SINGLE_DEVICE", "targetDeviceIds": []string{target}, "contentType": "FILE", "routeMode": "NODE_ONLY",
			"files":              []map[string]any{{"name": fmt.Sprintf("load-%02d.bin", index+1), "mimeType": "application/octet-stream", "size": 1, "sha256": make([]byte, sha256.Size), "chunkSize": 1, "chunkCount": 1}},
			"wrappedContentKeys": map[string]string{target: base64.StdEncoding.EncodeToString(wrappedKey)},
		}
		var created transferResponse
		if err := client.requestWithHeaders(ctx, http.MethodPost, "/api/transfers", tokens[0], request, map[string]string{"Idempotency-Key": randomUUID()}, http.StatusCreated, &created); err != nil {
			closeConnections(connections)
			return scenarioState{}, fmt.Errorf("create active transfer %d: %w", index+1, err)
		}
		if created.ID == "" || created.ContentType != "FILE" {
			closeConnections(connections)
			return scenarioState{}, fmt.Errorf("create active transfer %d returned invalid transfer", index+1)
		}
		transferIDs = append(transferIDs, created.ID)
	}
	deviceIDs := make([]string, len(devices))
	for index, device := range devices {
		deviceIDs[index] = device.id
	}
	return scenarioState{deviceIDs: deviceIDs, connections: connections, transferIDs: transferIDs, token: tokens[0]}, nil
}

func (client *apiClient) verifyScenario(ctx context.Context, state scenarioState) (scenarioResult, error) {
	var listed []deviceResponse
	if err := client.request(ctx, http.MethodGet, "/api/devices", state.token, nil, http.StatusOK, &listed); err != nil {
		return scenarioResult{}, fmt.Errorf("verify registered devices: %w", err)
	}
	known := make(map[string]struct{}, len(listed))
	for _, device := range listed {
		known[device.ID] = struct{}{}
	}
	registered := 0
	for _, id := range state.deviceIDs {
		if _, exists := known[id]; exists {
			registered++
		}
	}
	var online atomic.Int64
	var heartbeatChecks sync.WaitGroup
	for _, connection := range state.connections {
		heartbeatChecks.Add(1)
		go func(connection *presenceConnection) {
			defer heartbeatChecks.Done()
			if err := connection.heartbeat(ctx); err != nil {
				connection.alive.Store(false)
				return
			}
			connection.alive.Store(true)
			online.Add(1)
		}(connection)
	}
	heartbeatChecks.Wait()
	active := 0
	for _, id := range state.transferIDs {
		var transfer transferResponse
		if err := client.request(ctx, http.MethodGet, "/api/transfers/"+id, state.token, nil, http.StatusOK, &transfer); err != nil {
			return scenarioResult{RegisteredDevices: registered, OnlineDevices: int(online.Load()), ActiveTransfers: active}, fmt.Errorf("verify transfer %s: %w", id, err)
		}
		if transfer.ContentType == "FILE" && activeTransferStatus(transfer.Status) {
			active++
		}
	}
	return scenarioResult{RegisteredDevices: registered, OnlineDevices: int(online.Load()), ActiveTransfers: active}, nil
}

func activeTransferStatus(status string) bool {
	switch status {
	case "CREATED", "CHECKING_ROUTE", "QUEUED", "UPLOADING_TO_NODE", "AVAILABLE_ON_NODE", "WAITING_FOR_TARGET", "WAITING_FOR_NODE", "WAITING_FOR_LAN", "TRANSFERRING_LAN", "DOWNLOADING_FROM_NODE", "VERIFYING", "PAUSED":
		return true
	default:
		return false
	}
}

func (client *apiClient) login(ctx context.Context, username, password string) (tokenPair, error) {
	var pair tokenPair
	err := client.request(ctx, http.MethodPost, "/api/auth/login", "", map[string]string{"identifier": username, "password": password}, http.StatusOK, &pair)
	return pair, err
}

func (client *apiClient) connectPresence(ctx context.Context, token string) (*presenceConnection, error) {
	endpoint, err := url.Parse(client.baseURL)
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = "/ws"
	query := endpoint.Query()
	query.Set("access_token", token)
	query.Set("protocolVersion", "1.1")
	query.Set("clientVersion", "loadtest-v1.1")
	endpoint.RawQuery = query.Encode()
	connection, _, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{Subprotocols: []string{"nexdrop.v1"}})
	if err != nil {
		return nil, err
	}
	readContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, payload, err := connection.Read(readContext)
	if err != nil {
		connection.CloseNow()
		return nil, err
	}
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &message) != nil || message.Type != "connected" {
		connection.CloseNow()
		return nil, errors.New("presence connection did not acknowledge connected state")
	}
	presence := &presenceConnection{connection: connection}
	if err := presence.heartbeat(ctx); err != nil {
		connection.CloseNow()
		return nil, err
	}
	presence.alive.Store(true)
	background, stop := context.WithCancel(context.Background())
	presence.cancel = stop
	go presence.heartbeatLoop(background)
	return presence, nil
}

func (connection *presenceConnection) heartbeat(ctx context.Context) error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	writeContext, writeCancel := context.WithTimeout(ctx, 5*time.Second)
	err := connection.connection.Write(writeContext, websocket.MessageText, []byte(`{"type":"heartbeat"}`))
	writeCancel()
	if err != nil {
		return err
	}
	for {
		readContext, readCancel := context.WithTimeout(ctx, 5*time.Second)
		_, payload, err := connection.connection.Read(readContext)
		readCancel()
		if err != nil {
			return err
		}
		var message struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(payload, &message) != nil {
			return errors.New("presence returned invalid JSON")
		}
		if message.Type == "heartbeat_ack" {
			return nil
		}
	}
}

func (connection *presenceConnection) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := connection.heartbeat(ctx); err != nil {
				connection.alive.Store(false)
				return
			}
			connection.alive.Store(true)
		}
	}
}

func (connection *presenceConnection) close() {
	if connection.cancel != nil {
		connection.cancel()
	}
	connection.alive.Store(false)
	connection.connection.CloseNow()
}

func (client *apiClient) request(ctx context.Context, method, path, token string, body any, expected int, output any) error {
	return client.requestWithHeaders(ctx, method, path, token, body, nil, expected, output)
}

func (client *apiClient) requestWithHeaders(ctx context.Context, method, path, token string, body any, headers map[string]string, expected int, output any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.nexdrop.v1+json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != expected {
		return fmt.Errorf("%s %s returned %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(content)))
	}
	if output != nil && len(content) > 0 {
		if err := json.Unmarshal(content, output); err != nil {
			return err
		}
	}
	return nil
}

func runLatency(ctx context.Context, config configuration) latencyResult {
	client := &http.Client{Timeout: 5 * time.Second}
	jobs := make(chan struct{})
	durations := make([]time.Duration, 0, config.requests)
	var failures atomic.Int64
	var mutex sync.Mutex
	var workers sync.WaitGroup
	for range config.concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range jobs {
				started := time.Now()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(config.baseURL, "/")+"/api/version", nil)
				var response *http.Response
				if err == nil {
					response, err = client.Do(request)
				}
				duration := time.Since(started)
				if err != nil || response == nil || response.StatusCode != http.StatusOK {
					failures.Add(1)
				}
				if response != nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				mutex.Lock()
				durations = append(durations, duration)
				mutex.Unlock()
			}
		}()
	}
	dispatched := 0
dispatch:
	for dispatched < config.requests {
		select {
		case jobs <- struct{}{}:
			dispatched++
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	if missing := config.requests - dispatched; missing > 0 {
		failures.Add(int64(missing))
	}
	p50, p95 := percentile(durations, 50), percentile(durations, 95)
	failureCount := failures.Load()
	return latencyResult{
		Requests: config.requests, Concurrency: config.concurrency, Failures: failureCount,
		SuccessRate: float64(config.requests-int(failureCount)) / float64(config.requests),
		P50:         p50, P95: p95, P50Millis: float64(p50) / float64(time.Millisecond), P95Millis: float64(p95) / float64(time.Millisecond),
	}
}

func percentile(values []time.Duration, percentage int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[(len(ordered)*percentage-1)/100]
}

func meetsLatencyTarget(result latencyResult, maximumP95 time.Duration) bool {
	return result.Failures == 0 && result.P95 < maximumP95
}

func randomUUID() string {
	value := make([]byte, 16)
	_, _ = rand.Read(value)
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	hexValue := hex.EncodeToString(value)
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:]
}

func closeConnections(connections []*presenceConnection) {
	for _, connection := range connections {
		connection.close()
	}
}

func exitUsage(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
