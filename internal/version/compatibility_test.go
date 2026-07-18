package version

import "testing"

func TestSupportsCurrentAndPreviousProtocol(t *testing.T) {
	for _, value := range []string{"1", "1.0", "1.1"} {
		if !SupportedProtocol(value) {
			t.Fatalf("protocol %q was rejected", value)
		}
	}
	for _, value := range []string{"", "0.9", "1.2", "2.0"} {
		if SupportedProtocol(value) {
			t.Fatalf("protocol %q was accepted", value)
		}
	}
}

func TestClientVersionIncludesProductAndSupportedRelease(t *testing.T) {
	for _, value := range []string{"web-v1", "windows-v1.0", "android-v1.1"} {
		if !SupportedClient(value) {
			t.Fatalf("client %q was rejected", value)
		}
	}
	for _, value := range []string{"1.1", "web-v0.9", "web-v1.2", "web-v2"} {
		if SupportedClient(value) {
			t.Fatalf("client %q was accepted", value)
		}
	}
}

func TestCurrentIncludesProductAndBuildInformation(t *testing.T) {
	information := Current()
	if information.ProductVersion != "1.0.4" {
		t.Fatalf("product version = %q", information.ProductVersion)
	}
	if information.BuildCommit == "" {
		t.Fatal("build commit is empty")
	}
}
