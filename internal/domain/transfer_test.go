package domain

import "testing"

func TestTransferStatusAllowsOnlyDocumentedTransitions(t *testing.T) {
	tests := []struct {
		from TransferStatus
		to   TransferStatus
		want bool
	}{
		{TransferCreated, TransferCheckingRoute, true},
		{TransferCheckingRoute, TransferQueued, true},
		{TransferWaitingForLAN, TransferCheckingRoute, true},
		{TransferTransferringLAN, TransferVerifying, true},
		{TransferDelivered, TransferRead, true},
		{TransferQueued, TransferPaused, true},
		{TransferPaused, TransferQueued, true},
		{TransferCreated, TransferDelivered, false},
		{TransferQueued, TransferRead, false},
		{TransferRead, TransferCheckingRoute, false},
		{TransferFailed, TransferCheckingRoute, false},
	}
	for _, test := range tests {
		if got := test.from.CanTransitionTo(test.to); got != test.want {
			t.Errorf("%s -> %s = %t, want %t", test.from, test.to, got, test.want)
		}
	}
}
