package domain

const DefaultLargeFileThreshold int64 = 100 * 1024 * 1024

type RouteMode string

const (
	RouteModeAutomatic RouteMode = "AUTOMATIC"
	RouteModeLANOnly   RouteMode = "LAN_ONLY"
	RouteModeNodeOnly  RouteMode = "NODE_ONLY"
	RouteModeWaitLAN   RouteMode = "WAIT_LAN"
)

type SelectedRoute string

const (
	SelectedRouteNone       SelectedRoute = "NONE"
	SelectedRouteLAN        SelectedRoute = "LAN"
	SelectedRouteNode       SelectedRoute = "NODE"
	SelectedRouteWaitingLAN SelectedRoute = "WAITING_LAN"
	SelectedRouteDraft      SelectedRoute = "LOCAL_DRAFT"
)

type RouteRequest struct {
	Mode                  RouteMode
	LANAvailable          bool
	NodeAvailable         bool
	TextContent           bool
	FileSize              int64
	LargeFileThreshold    int64
	AllowLargeFileViaNode bool
}

func SelectRoute(request RouteRequest) SelectedRoute {
	threshold := request.LargeFileThreshold
	if threshold <= 0 {
		threshold = DefaultLargeFileThreshold
	}
	isLarge := !request.TextContent && request.FileSize > threshold

	switch request.Mode {
	case RouteModeLANOnly:
		if request.LANAvailable {
			return SelectedRouteLAN
		}
		return SelectedRouteWaitingLAN
	case RouteModeNodeOnly:
		if !request.NodeAvailable || (isLarge && !request.AllowLargeFileViaNode) {
			return SelectedRouteNone
		}
		return SelectedRouteNode
	case RouteModeWaitLAN:
		if request.LANAvailable {
			return SelectedRouteLAN
		}
		return SelectedRouteWaitingLAN
	}

	if request.LANAvailable {
		return SelectedRouteLAN
	}
	if request.NodeAvailable {
		if !isLarge || request.AllowLargeFileViaNode {
			return SelectedRouteNode
		}
		return SelectedRouteWaitingLAN
	}
	if isLarge {
		return SelectedRouteWaitingLAN
	}
	return SelectedRouteDraft
}
