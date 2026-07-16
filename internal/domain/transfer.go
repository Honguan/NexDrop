package domain

type TransferStatus string

const (
	TransferCreated           TransferStatus = "CREATED"
	TransferCheckingRoute     TransferStatus = "CHECKING_ROUTE"
	TransferWaitingForTarget  TransferStatus = "WAITING_FOR_TARGET"
	TransferWaitingForNode    TransferStatus = "WAITING_FOR_NODE"
	TransferWaitingForLAN     TransferStatus = "WAITING_FOR_LAN"
	TransferQueued            TransferStatus = "QUEUED"
	TransferUploadingToNode   TransferStatus = "UPLOADING_TO_NODE"
	TransferAvailableOnNode   TransferStatus = "AVAILABLE_ON_NODE"
	TransferDownloading       TransferStatus = "DOWNLOADING_FROM_NODE"
	TransferTransferringLAN   TransferStatus = "TRANSFERRING_LAN"
	TransferPaused            TransferStatus = "PAUSED"
	TransferVerifying         TransferStatus = "VERIFYING"
	TransferDelivered         TransferStatus = "DELIVERED"
	TransferRead              TransferStatus = "READ"
	TransferFailed            TransferStatus = "FAILED"
	TransferCancelled         TransferStatus = "CANCELLED"
	TransferExpired           TransferStatus = "EXPIRED"
	TransferSourceFileMissing TransferStatus = "SOURCE_FILE_MISSING"
	TransferSourceFileChanged TransferStatus = "SOURCE_FILE_CHANGED"
)

var terminalTransferStatuses = map[TransferStatus]bool{
	TransferRead:      true,
	TransferFailed:    true,
	TransferCancelled: true,
	TransferExpired:   true,
}

var transferStatusTransitions = map[TransferStatus]map[TransferStatus]bool{
	TransferCreated: {TransferCheckingRoute: true},
	TransferCheckingRoute: {
		TransferTransferringLAN: true, TransferQueued: true, TransferWaitingForTarget: true,
		TransferWaitingForNode: true, TransferWaitingForLAN: true, TransferFailed: true, TransferCancelled: true,
	},
	TransferQueued:          {TransferUploadingToNode: true, TransferPaused: true, TransferCancelled: true, TransferExpired: true},
	TransferUploadingToNode: {TransferAvailableOnNode: true, TransferPaused: true, TransferFailed: true, TransferCancelled: true},
	TransferAvailableOnNode: {TransferDownloading: true, TransferPaused: true, TransferExpired: true, TransferCancelled: true},
	TransferTransferringLAN: {TransferVerifying: true, TransferPaused: true, TransferFailed: true, TransferCancelled: true},
	TransferDownloading:     {TransferVerifying: true, TransferPaused: true, TransferFailed: true, TransferCancelled: true},
	TransferVerifying:       {TransferDelivered: true, TransferFailed: true},
	TransferDelivered:       {TransferRead: true},
	TransferWaitingForLAN: {
		TransferCheckingRoute: true, TransferSourceFileMissing: true, TransferSourceFileChanged: true,
		TransferPaused: true, TransferCancelled: true, TransferExpired: true,
	},
	TransferWaitingForTarget:  {TransferCheckingRoute: true, TransferPaused: true, TransferCancelled: true, TransferExpired: true},
	TransferWaitingForNode:    {TransferCheckingRoute: true, TransferPaused: true, TransferCancelled: true, TransferExpired: true},
	TransferPaused:            {TransferQueued: true},
	TransferSourceFileMissing: {TransferCheckingRoute: true},
	TransferSourceFileChanged: {TransferCheckingRoute: true},
}

func (status TransferStatus) IsTerminal() bool {
	return terminalTransferStatuses[status]
}

func (status TransferStatus) CanTransitionTo(next TransferStatus) bool {
	return transferStatusTransitions[status][next]
}
