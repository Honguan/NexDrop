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

func (status TransferStatus) IsTerminal() bool {
	return terminalTransferStatuses[status]
}
