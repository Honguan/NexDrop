package lan

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"nexdrop/internal/version"
)

const (
	ServiceType       = "_nexdrop._tcp"
	ProtocolVersion   = version.CurrentProtocol
	FallbackPort      = 53317
	discoveryMagic    = "NEXDROP_DISCOVERY_V1"
	defaultDiscoverIn = 5 * time.Second
)

type Advertisement struct {
	ShortDeviceID  string `json:"deviceId"`
	ServiceVersion string `json:"serviceVersion"`
	Protocol       string `json:"protocolVersion"`
	Port           int    `json:"port"`
	Challenge      string `json:"challenge"`
	Address        string `json:"-"`
}

func NewAdvertisement(shortDeviceID, serviceVersion string, port int) (Advertisement, error) {
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return Advertisement{}, fmt.Errorf("generate discovery challenge: %w", err)
	}
	value := Advertisement{ShortDeviceID: shortDeviceID, ServiceVersion: serviceVersion, Protocol: ProtocolVersion, Port: port, Challenge: base64.RawURLEncoding.EncodeToString(challenge)}
	if err := value.Validate(); err != nil {
		return Advertisement{}, err
	}
	return value, nil
}

func (value Advertisement) Validate() error {
	if len(value.ShortDeviceID) < 6 || len(value.ShortDeviceID) > 32 || !identifier(value.ShortDeviceID) || value.ServiceVersion == "" || len(value.ServiceVersion) > 32 || value.Protocol == "" || len(value.Protocol) > 16 || value.Port < 1 || value.Port > 65535 {
		return errors.New("invalid LAN advertisement")
	}
	challenge, err := base64.RawURLEncoding.DecodeString(value.Challenge)
	if err != nil || len(challenge) != 16 {
		return errors.New("invalid LAN challenge")
	}
	return nil
}

type Advertiser struct {
	mdns *zeroconf.Server
	udp  *net.UDPConn
	done chan struct{}
	once sync.Once
}

func StartAdvertiser(value Advertisement) (*Advertiser, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	ips, err := localAddresses()
	if err != nil {
		return nil, err
	}
	server, err := zeroconf.RegisterProxy("nexdrop-"+value.ShortDeviceID, ServiceType, "local.", value.Port, "nexdrop-"+value.ShortDeviceID+".local.", ips, value.text(), nil)
	if err != nil {
		return nil, fmt.Errorf("advertise mDNS service: %w", err)
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: FallbackPort})
	if err != nil {
		server.Shutdown()
		return nil, fmt.Errorf("listen for discovery broadcasts: %w", err)
	}
	advertiser := &Advertiser{mdns: server, udp: udp, done: make(chan struct{})}
	go advertiser.respond(value)
	return advertiser, nil
}

func (advertiser *Advertiser) Close() error {
	var err error
	advertiser.once.Do(func() {
		close(advertiser.done)
		advertiser.mdns.Shutdown()
		err = advertiser.udp.Close()
	})
	return err
}

func Discover(ctx context.Context) ([]Advertisement, error) {
	if ctx == nil {
		return nil, errors.New("discovery context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultDiscoverIn)
	defer cancel()
	results := make(chan Advertisement, 16)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); discoverMDNS(ctx, results) }()
	go func() { defer workers.Done(); discoverBroadcast(ctx, results) }()
	go func() { workers.Wait(); close(results) }()
	unique := make(map[string]Advertisement)
	for value := range results {
		if value.Validate() == nil {
			unique[value.ShortDeviceID] = value
		}
	}
	values := make([]Advertisement, 0, len(unique))
	for _, value := range unique {
		values = append(values, value)
	}
	return values, nil
}

func discoverMDNS(ctx context.Context, output chan<- Advertisement) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return
	}
	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			value, err := advertisementFromText(entry.Text)
			if err != nil || value.Port != entry.Port {
				continue
			}
			addresses := append(entry.AddrIPv4, entry.AddrIPv6...)
			if len(addresses) == 0 {
				continue
			}
			value.Address = addresses[0].String()
			select {
			case output <- value:
			case <-ctx.Done():
				return
			}
		}
	}()
	_ = resolver.Browse(ctx, ServiceType, "local.", entries)
	<-ctx.Done()
}

type discoveryPacket struct {
	Magic         string         `json:"magic"`
	Type          string         `json:"type"`
	Nonce         string         `json:"nonce"`
	Advertisement *Advertisement `json:"advertisement,omitempty"`
}

func (advertiser *Advertiser) respond(value Advertisement) {
	buffer := make([]byte, 2048)
	for {
		_ = advertiser.udp.SetReadDeadline(time.Now().Add(time.Second))
		read, source, err := advertiser.udp.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-advertiser.done:
				return
			default:
				continue
			}
		}
		var query discoveryPacket
		if json.Unmarshal(buffer[:read], &query) != nil || query.Magic != discoveryMagic || query.Type != "query" || query.Nonce == "" {
			continue
		}
		response := discoveryPacket{Magic: discoveryMagic, Type: "response", Nonce: query.Nonce, Advertisement: &value}
		encoded, _ := json.Marshal(response)
		_, _ = advertiser.udp.WriteToUDP(encoded, source)
	}
}

func discoverBroadcast(ctx context.Context, output chan<- Advertisement) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return
	}
	defer connection.Close()
	nonceBytes := make([]byte, 16)
	_, _ = rand.Read(nonceBytes)
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	query, _ := json.Marshal(discoveryPacket{Magic: discoveryMagic, Type: "query", Nonce: nonce})
	_, _ = connection.WriteToUDP(query, &net.UDPAddr{IP: net.IPv4bcast, Port: FallbackPort})
	buffer := make([]byte, 2048)
	for {
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(defaultDiscoverIn)
		}
		_ = connection.SetReadDeadline(deadline)
		read, source, err := connection.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		var response discoveryPacket
		if json.Unmarshal(buffer[:read], &response) != nil || response.Magic != discoveryMagic || response.Type != "response" || response.Nonce != nonce || response.Advertisement == nil {
			continue
		}
		value := *response.Advertisement
		value.Address = source.IP.String()
		select {
		case output <- value:
		case <-ctx.Done():
			return
		}
	}
}

func (value Advertisement) text() []string {
	return []string{"id=" + value.ShortDeviceID, "sv=" + value.ServiceVersion, "pv=" + value.Protocol, "port=" + strconv.Itoa(value.Port), "challenge=" + value.Challenge}
}

func advertisementFromText(text []string) (Advertisement, error) {
	values := make(map[string]string, len(text))
	for _, item := range text {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	port, _ := strconv.Atoi(values["port"])
	result := Advertisement{ShortDeviceID: values["id"], ServiceVersion: values["sv"], Protocol: values["pv"], Port: port, Challenge: values["challenge"]}
	return result, result.Validate()
}

func localAddresses() ([]string, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("list network addresses: %w", err)
	}
	result := make([]string, 0)
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			result = append(result, ip.String())
		}
	}
	if len(result) == 0 {
		return nil, errors.New("no LAN address available")
	}
	return result, nil
}

func identifier(value string) bool {
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}
