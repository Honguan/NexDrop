package lan

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestAdvertisementTextContainsOnlyProtocolFields(t *testing.T) {
	value := Advertisement{ShortDeviceID: "device01", ServiceVersion: "1.2.3", Protocol: ProtocolVersion, Port: 4242, Challenge: base64.RawURLEncoding.EncodeToString(make([]byte, 16))}
	parsed, err := advertisementFromText(value.text())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, value) {
		t.Fatalf("parsed advertisement = %+v, want %+v", parsed, value)
	}
	if len(value.text()) != 5 {
		t.Fatalf("TXT field count = %d, want 5", len(value.text()))
	}
}

func TestAdvertisementRejectsPrivateOrMalformedFields(t *testing.T) {
	validChallenge := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	values := []Advertisement{
		{ShortDeviceID: "bad id", ServiceVersion: "1", Protocol: "1", Port: 1, Challenge: validChallenge},
		{ShortDeviceID: "device01", ServiceVersion: "1", Protocol: "1", Port: 0, Challenge: validChallenge},
		{ShortDeviceID: "device01", ServiceVersion: "1", Protocol: "1", Port: 1, Challenge: "bad"},
	}
	for index, value := range values {
		if value.Validate() == nil {
			t.Fatalf("advertisement %d was accepted", index)
		}
	}
}
