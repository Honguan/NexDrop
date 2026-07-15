package domain

type LocalChannelState string

const (
	LocalOffline    LocalChannelState = "OFFLINE"
	LocalNodeOnly   LocalChannelState = "NODE_ONLY"
	LocalLANOnly    LocalChannelState = "LAN_ONLY"
	LocalLANAndNode LocalChannelState = "LAN_AND_NODE"
)

func DetermineLocalChannelState(lanAvailable, nodeAvailable bool) LocalChannelState {
	switch {
	case lanAvailable && nodeAvailable:
		return LocalLANAndNode
	case lanAvailable:
		return LocalLANOnly
	case nodeAvailable:
		return LocalNodeOnly
	default:
		return LocalOffline
	}
}

type TargetState string

const (
	TargetUnavailable TargetState = "TARGET_UNAVAILABLE"
	TargetNode        TargetState = "TARGET_NODE_AVAILABLE"
	TargetLAN         TargetState = "TARGET_LAN_AVAILABLE"
	TargetLANAndNode  TargetState = "TARGET_LAN_AND_NODE"
)

func DetermineTargetState(lanAvailable, nodeAvailable bool) TargetState {
	switch {
	case lanAvailable && nodeAvailable:
		return TargetLANAndNode
	case lanAvailable:
		return TargetLAN
	case nodeAvailable:
		return TargetNode
	default:
		return TargetUnavailable
	}
}
