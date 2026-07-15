package domain

import "testing"

func TestDetermineLocalChannelState(t *testing.T) {
	tests := []struct {
		name string
		lan  bool
		node bool
		want LocalChannelState
	}{
		{"offline", false, false, LocalOffline},
		{"node only", false, true, LocalNodeOnly},
		{"LAN only", true, false, LocalLANOnly},
		{"LAN and node", true, true, LocalLANAndNode},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DetermineLocalChannelState(test.lan, test.node); got != test.want {
				t.Fatalf("DetermineLocalChannelState() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDetermineTargetState(t *testing.T) {
	tests := []struct {
		lan, node bool
		want      TargetState
	}{
		{false, false, TargetUnavailable},
		{false, true, TargetNode},
		{true, false, TargetLAN},
		{true, true, TargetLANAndNode},
	}

	for _, test := range tests {
		if got := DetermineTargetState(test.lan, test.node); got != test.want {
			t.Fatalf("DetermineTargetState(%t, %t) = %q, want %q", test.lan, test.node, got, test.want)
		}
	}
}
