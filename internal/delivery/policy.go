package delivery

import (
	"sort"
	"time"
)

type Priority int

const (
	PriorityDeferred Priority = iota
	PriorityNormal
	PriorityUrgent
)

type State string

const (
	StateQueued            State = "QUEUED"
	StateMetadataAvailable State = "METADATA_AVAILABLE"
	StateWaitingDevice     State = "WAITING_FOR_DEVICE"
	StateWaitingNetwork    State = "WAITING_FOR_NETWORK_POLICY"
	StateWaitingPower      State = "WAITING_FOR_POWER"
	StateDownloading       State = "DOWNLOADING"
	StatePausedSystem      State = "PAUSED_BY_SYSTEM"
	StateDelivered         State = "DELIVERED"
	StateRead              State = "READ"
	StateExpired           State = "EXPIRED"
	StateCancelled         State = "CANCELLED"
)

type Policy struct {
	ExpiresAt         time.Time
	WiFiOnly          bool
	ChargingOnly      bool
	MaxMobileDataSize int64
	AutomaticDownload bool
	ForegroundOnly    bool
}

type Environment struct {
	Now          time.Time
	Online       bool
	WiFi         bool
	Metered      bool
	Charging     bool
	Foreground   bool
	StorageBytes int64
	LowBattery   bool
	ThermalHigh  bool
}

type Item struct {
	ID                   string
	Priority             Priority
	Size                 int64
	CreatedAt            time.Time
	Policy               Policy
	MetadataSynchronized bool
}

type Decision struct {
	State        State
	MetadataOnly bool
	ReasonCode   string
}

func Evaluate(item Item, environment Environment) Decision {
	if !item.Policy.ExpiresAt.IsZero() && !environment.Now.Before(item.Policy.ExpiresAt) {
		return Decision{State: StateExpired, ReasonCode: "CONTENT_EXPIRED"}
	}
	if !environment.Online {
		return Decision{State: StateWaitingDevice, MetadataOnly: true, ReasonCode: "DEVICE_OFFLINE"}
	}
	if !item.MetadataSynchronized {
		return Decision{State: StateMetadataAvailable, MetadataOnly: true, ReasonCode: "METADATA_FIRST"}
	}
	if !item.Policy.AutomaticDownload {
		return Decision{State: StateMetadataAvailable, MetadataOnly: true, ReasonCode: "AUTOMATIC_DOWNLOAD_DISABLED"}
	}
	if item.Policy.ForegroundOnly && !environment.Foreground {
		return Decision{State: StatePausedSystem, MetadataOnly: true, ReasonCode: "FOREGROUND_REQUIRED"}
	}
	if item.Policy.WiFiOnly && !environment.WiFi {
		return Decision{State: StateWaitingNetwork, MetadataOnly: true, ReasonCode: "WIFI_REQUIRED"}
	}
	if environment.Metered && item.Policy.MaxMobileDataSize >= 0 && item.Size > item.Policy.MaxMobileDataSize {
		return Decision{State: StateWaitingNetwork, MetadataOnly: true, ReasonCode: "MOBILE_DATA_LIMIT"}
	}
	if item.Policy.ChargingOnly && !environment.Charging {
		return Decision{State: StateWaitingPower, MetadataOnly: true, ReasonCode: "CHARGING_REQUIRED"}
	}
	if environment.LowBattery || environment.ThermalHigh {
		return Decision{State: StatePausedSystem, MetadataOnly: true, ReasonCode: "SYSTEM_RESOURCE_LIMIT"}
	}
	if environment.StorageBytes >= 0 && item.Size > environment.StorageBytes {
		return Decision{State: StatePausedSystem, MetadataOnly: true, ReasonCode: "INSUFFICIENT_STORAGE"}
	}
	return Decision{State: StateDownloading}
}

func Order(items []Item) []Item {
	ordered := append([]Item(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].MetadataSynchronized != ordered[j].MetadataSynchronized {
			return !ordered[i].MetadataSynchronized
		}
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority > ordered[j].Priority
		}
		if ordered[i].Size != ordered[j].Size {
			return ordered[i].Size < ordered[j].Size
		}
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}
