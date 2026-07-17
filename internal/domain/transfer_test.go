package domain

import "testing"

func TestTransferStatusAllowsOnlyDocumentedTransitions(t *testing.T) {
	statuses := []TransferStatus{
		TransferCreated, TransferCheckingRoute, TransferWaitingForTarget, TransferWaitingForNode,
		TransferWaitingForLAN, TransferQueued, TransferUploadingToNode, TransferAvailableOnNode,
		TransferDownloading, TransferTransferringLAN, TransferPaused, TransferVerifying,
		TransferDelivered, TransferRead, TransferFailed, TransferCancelled, TransferExpired,
		TransferSourceFileMissing, TransferSourceFileChanged,
	}
	allowed := map[TransferStatus]map[TransferStatus]bool{
		TransferCreated: {TransferCheckingRoute: true},
		TransferCheckingRoute: {
			TransferTransferringLAN: true, TransferQueued: true, TransferWaitingForTarget: true,
			TransferWaitingForNode: true, TransferWaitingForLAN: true, TransferFailed: true, TransferCancelled: true,
		},
		TransferQueued:          {TransferUploadingToNode: true, TransferCancelled: true, TransferExpired: true},
		TransferUploadingToNode: {TransferAvailableOnNode: true, TransferPaused: true, TransferFailed: true, TransferCancelled: true},
		TransferAvailableOnNode: {TransferDownloading: true, TransferExpired: true, TransferCancelled: true},
		TransferTransferringLAN: {TransferVerifying: true, TransferPaused: true, TransferFailed: true, TransferCancelled: true},
		TransferDownloading:     {TransferVerifying: true, TransferPaused: true, TransferFailed: true, TransferCancelled: true},
		TransferVerifying:       {TransferDelivered: true, TransferFailed: true},
		TransferDelivered:       {TransferRead: true},
		TransferWaitingForLAN: {
			TransferCheckingRoute: true, TransferSourceFileMissing: true, TransferSourceFileChanged: true,
			TransferPaused: true, TransferCancelled: true, TransferExpired: true,
		},
		TransferSourceFileMissing: {TransferCheckingRoute: true},
		TransferSourceFileChanged: {TransferCheckingRoute: true},
	}
	for _, from := range statuses {
		for _, to := range statuses {
			if got, want := from.CanTransitionTo(to), allowed[from][to]; got != want {
				t.Errorf("%s -> %s = %t, want %t", from, to, got, want)
			}
		}
	}
}

func TestTerminalTransferStatusesCannotResume(t *testing.T) {
	for _, status := range []TransferStatus{TransferRead, TransferFailed, TransferCancelled, TransferExpired} {
		if !status.IsTerminal() {
			t.Errorf("%s is not terminal", status)
		}
		if status.CanTransitionTo(TransferCheckingRoute) {
			t.Errorf("%s resumed from a terminal state", status)
		}
	}
}
