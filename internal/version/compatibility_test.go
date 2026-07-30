package version

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestSupportsCurrentAndPreviousProtocol(t *testing.T) {
	for _, value := range []string{"1", "1.0", "1.1", "1.2"} {
		if !SupportedProtocol(value) {
			t.Fatalf("protocol %q was rejected", value)
		}
	}
	for _, value := range []string{"", "0.9", "1.3", "2.0"} {
		if SupportedProtocol(value) {
			t.Fatalf("protocol %q was accepted", value)
		}
	}
}

func TestClientVersionIncludesProductAndSupportedRelease(t *testing.T) {
	for _, value := range []string{"web-v1", "windows-v1.0", "android-v1.1", "extension-v1.2"} {
		if !SupportedClient(value) {
			t.Fatalf("client %q was rejected", value)
		}
	}
	for _, value := range []string{"1.1", "web-v0.9", "web-v1.3", "web-v2"} {
		if SupportedClient(value) {
			t.Fatalf("client %q was accepted", value)
		}
	}
}

func TestCurrentIncludesProductAndBuildInformation(t *testing.T) {
	information := Current()
	if information.ProductVersion != "2.0.4" {
		t.Fatalf("product version = %q", information.ProductVersion)
	}
	if information.BuildCommit == "" {
		t.Fatal("build commit is empty")
	}
}

func TestCurrentPublishesStableCapabilityDocument(t *testing.T) {
	t.Setenv("NEXDROP_NODE_ID", "node-0123456789abcdef0123456789abcdef")
	information := Current()

	if information.NodeIdentity == "" || information.CapabilitySchemaVersion != 1 || information.VersionFingerprint == "" {
		t.Fatalf("capability metadata = %+v", information)
	}
	if !reflect.DeepEqual(information.Capabilities, SupportedCapabilities()) {
		t.Fatalf("capabilities = %v, want %v", information.Capabilities, SupportedCapabilities())
	}
	if information.Limits.MaxChunkSize != 8*1024*1024 || information.Limits.MaxParallelChunks != 3 || information.Limits.MaxRecipients != 100 {
		t.Fatalf("limits = %+v", information.Limits)
	}
}

func TestCapabilityRegistryHasFallbackAndStableError(t *testing.T) {
	definitions := Registry()
	if len(definitions) == 0 {
		t.Fatal("capability registry is empty")
	}
	seen := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		if definition.ID == "" || len(definition.Parties) == 0 || definition.Fallback == "" || definition.UnavailableError != CapabilityUnavailableCode {
			t.Fatalf("incomplete capability definition = %+v", definition)
		}
		if seen[definition.ID] {
			t.Fatalf("duplicate capability %q", definition.ID)
		}
		seen[definition.ID] = true
	}
	if err := RequireCapabilities([]string{CapabilityNegotiation}, CapabilityNegotiation); err != nil {
		t.Fatalf("supported capability rejected: %v", err)
	}
	var unavailable CapabilityUnavailableError
	if err := RequireCapabilities(nil, CapabilityNegotiation); !errors.As(err, &unavailable) || unavailable.Code != CapabilityUnavailableCode || unavailable.Capability != CapabilityNegotiation {
		t.Fatalf("unavailable capability error = %#v", err)
	}
}

func TestNegotiatedCapabilitiesIgnoreUnknownIdentifiers(t *testing.T) {
	got := NegotiateCapabilities([]string{CapabilityNegotiation, "future_unknown", StructuredErrors})
	want := []string{CapabilityNegotiation, StructuredErrors}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("negotiated capabilities = %v, want %v", got, want)
	}
}

func TestMixedVersionFixturesUseSafeCapabilityFallbacks(t *testing.T) {
	current := readInformationFixture(t, "testdata/node-current.json")
	previous := readInformationFixture(t, "testdata/node-previous.json")
	tests := []struct {
		name   string
		node   Information
		client []string
		want   []string
	}{
		{name: "current/current", node: current, client: []string{CapabilityNegotiation, StructuredErrors}, want: []string{CapabilityNegotiation, StructuredErrors}},
		{name: "current/previous", node: current, client: nil, want: nil},
		{name: "previous/current", node: previous, client: []string{CapabilityNegotiation, StructuredErrors}, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MutualCapabilities(test.node.Capabilities, test.client)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("mutual capabilities = %v, want %v", got, test.want)
			}
		})
	}
}

func readInformationFixture(t *testing.T, path string) Information {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var information Information
	if err := json.Unmarshal(data, &information); err != nil {
		t.Fatal(err)
	}
	return information
}
