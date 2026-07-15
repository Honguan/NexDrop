package secure

import (
	"bytes"
	"errors"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	recipient, err := GenerateDeviceKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("private NexDrop payload")
	associatedData := []byte("transfer-id:device-id")

	envelope, err := EncryptForDevice(plaintext, recipient.PublicKey, associatedData)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope.Ciphertext, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}

	got, err := DecryptForDevice(envelope, recipient.PrivateKey, associatedData)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("DecryptForDevice() = %q, want %q", got, plaintext)
	}
}

func TestEnvelopeCannotBeDecryptedByAnotherDevice(t *testing.T) {
	recipient, _ := GenerateDeviceKeyPair()
	otherDevice, _ := GenerateDeviceKeyPair()
	envelope, err := EncryptForDevice([]byte("secret"), recipient.PublicKey, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptForDevice(envelope, otherDevice.PrivateKey, nil)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("DecryptForDevice() error = %v, want ErrInvalidEnvelope", err)
	}
}

func TestEnvelopeRejectsTampering(t *testing.T) {
	recipient, _ := GenerateDeviceKeyPair()
	envelope, err := EncryptForDevice([]byte("secret"), recipient.PublicKey, []byte("bound metadata"))
	if err != nil {
		t.Fatal(err)
	}
	envelope.Ciphertext[0] ^= 0xff

	_, err = DecryptForDevice(envelope, recipient.PrivateKey, []byte("bound metadata"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("DecryptForDevice() error = %v, want ErrInvalidEnvelope", err)
	}
}

func TestEnvelopeRejectsChangedAssociatedData(t *testing.T) {
	recipient, _ := GenerateDeviceKeyPair()
	envelope, err := EncryptForDevice([]byte("secret"), recipient.PublicKey, []byte("device-a"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptForDevice(envelope, recipient.PrivateKey, []byte("device-b"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("DecryptForDevice() error = %v, want ErrInvalidEnvelope", err)
	}
}
