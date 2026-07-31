package delivery

import (
	"testing"
	"time"
)

func TestMetadataSynchronizesBeforeBody(t *testing.T) {
	decision := Evaluate(Item{ID: "1", Size: 10, Policy: Policy{AutomaticDownload: true}}, Environment{Now: time.Now(), Online: true, WiFi: true, Charging: true, Foreground: true, StorageBytes: 100})
	if decision.State != StateMetadataAvailable || !decision.MetadataOnly {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestLargeMeteredTransferWaitsForNetworkPolicy(t *testing.T) {
	decision := Evaluate(Item{ID: "1", Size: 100, MetadataSynchronized: true, Policy: Policy{AutomaticDownload: true, MaxMobileDataSize: 50}}, Environment{Now: time.Now(), Online: true, Metered: true, Foreground: true, StorageBytes: 1000})
	if decision.State != StateWaitingNetwork || decision.ReasonCode != "MOBILE_DATA_LIMIT" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestChargingOnlyTransferWaitsForPower(t *testing.T) {
	decision := Evaluate(Item{ID: "1", Size: 10, MetadataSynchronized: true, Policy: Policy{AutomaticDownload: true, ChargingOnly: true, MaxMobileDataSize: -1}}, Environment{Now: time.Now(), Online: true, WiFi: true, Foreground: true, StorageBytes: 100})
	if decision.State != StateWaitingPower {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestOrderPrioritizesMetadataThenUrgentSmallItems(t *testing.T) {
	now := time.Now()
	ordered := Order([]Item{
		{ID: "large", Priority: PriorityUrgent, Size: 100, CreatedAt: now, MetadataSynchronized: true},
		{ID: "meta", Priority: PriorityNormal, Size: 50, CreatedAt: now, MetadataSynchronized: false},
		{ID: "small", Priority: PriorityUrgent, Size: 10, CreatedAt: now, MetadataSynchronized: true},
	})
	if ordered[0].ID != "meta" || ordered[1].ID != "small" || ordered[2].ID != "large" {
		t.Fatalf("unexpected order: %#v", ordered)
	}
}
