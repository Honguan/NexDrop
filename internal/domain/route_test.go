package domain

import "testing"

func TestSelectRouteAutomatic(t *testing.T) {
	large := DefaultLargeFileThreshold + 1
	tests := []struct {
		name    string
		request RouteRequest
		want    SelectedRoute
	}{
		{"LAN has priority", RouteRequest{LANAvailable: true, NodeAvailable: true, FileSize: large}, SelectedRouteLAN},
		{"small file uses node", RouteRequest{NodeAvailable: true, FileSize: 1}, SelectedRouteNode},
		{"text uses node", RouteRequest{NodeAvailable: true, TextContent: true}, SelectedRouteNode},
		{"allowed large file uses node", RouteRequest{NodeAvailable: true, FileSize: large, AllowLargeFileViaNode: true}, SelectedRouteNode},
		{"restricted large file waits for LAN", RouteRequest{NodeAvailable: true, FileSize: large}, SelectedRouteWaitingLAN},
		{"offline small file becomes draft", RouteRequest{FileSize: 1}, SelectedRouteDraft},
		{"offline text becomes draft", RouteRequest{TextContent: true}, SelectedRouteDraft},
		{"offline large file waits for LAN", RouteRequest{FileSize: large}, SelectedRouteWaitingLAN},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SelectRoute(test.request); got != test.want {
				t.Fatalf("SelectRoute() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSelectRouteManualModeDoesNotFallBack(t *testing.T) {
	large := DefaultLargeFileThreshold + 1
	tests := []struct {
		name    string
		request RouteRequest
		want    SelectedRoute
	}{
		{"LAN only waits", RouteRequest{Mode: RouteModeLANOnly, NodeAvailable: true}, SelectedRouteWaitingLAN},
		{"node only unavailable", RouteRequest{Mode: RouteModeNodeOnly, LANAvailable: true}, SelectedRouteNone},
		{"node only rejects restricted large file", RouteRequest{Mode: RouteModeNodeOnly, NodeAvailable: true, FileSize: large}, SelectedRouteNone},
		{"wait LAN transfers when available", RouteRequest{Mode: RouteModeWaitLAN, LANAvailable: true}, SelectedRouteLAN},
		{"wait LAN remains queued", RouteRequest{Mode: RouteModeWaitLAN, NodeAvailable: true}, SelectedRouteWaitingLAN},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SelectRoute(test.request); got != test.want {
				t.Fatalf("SelectRoute() = %q, want %q", got, test.want)
			}
		})
	}
}
