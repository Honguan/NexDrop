package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

const (
	EnvelopeVersion = 1
	contentKeySize  = 32
)

var (
	ErrUnsupportedEnvelope = errors.New("unsupported encrypted envelope version")
	ErrInvalidEnvelope     = errors.New("invalid encrypted envelope")
)

type DeviceKeyPair struct {
	PrivateKey []byte
	PublicKey  []byte
}

type Envelope struct {
	Version            int    `json:"version"`
	EphemeralPublicKey []byte `json:"ephemeralPublicKey"`
	WrappedKeyNonce    []byte `json:"wrappedKeyNonce"`
	WrappedContentKey  []byte `json:"wrappedContentKey"`
	ContentNonce       []byte `json:"contentNonce"`
	Ciphertext         []byte `json:"ciphertext"`
}

func GenerateDeviceKeyPair() (DeviceKeyPair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return DeviceKeyPair{}, fmt.Errorf("generate device key: %w", err)
	}

	return DeviceKeyPair{
		PrivateKey: privateKey.Bytes(),
		PublicKey:  privateKey.PublicKey().Bytes(),
	}, nil
}

func EncryptForDevice(plaintext, recipientPublicKey, associatedData []byte) (Envelope, error) {
	recipient, err := ecdh.X25519().NewPublicKey(recipientPublicKey)
	if err != nil {
		return Envelope{}, fmt.Errorf("parse recipient public key: %w", err)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, fmt.Errorf("generate ephemeral key: %w", err)
	}
	sharedSecret, err := ephemeral.ECDH(recipient)
	if err != nil {
		return Envelope{}, fmt.Errorf("derive shared secret: %w", err)
	}

	contentKey := make([]byte, contentKeySize)
	if _, err := io.ReadFull(rand.Reader, contentKey); err != nil {
		return Envelope{}, fmt.Errorf("generate content key: %w", err)
	}
	contentNonce, ciphertext, err := seal(contentKey, plaintext, associatedData)
	if err != nil {
		return Envelope{}, err
	}

	wrappingKey := hkdfSHA256(sharedSecret, nil, []byte("nexdrop/private-transfer/v1"), contentKeySize)
	wrappedKeyNonce, wrappedContentKey, err := seal(wrappingKey, contentKey, associatedData)
	if err != nil {
		return Envelope{}, err
	}

	return Envelope{
		Version:            EnvelopeVersion,
		EphemeralPublicKey: ephemeral.PublicKey().Bytes(),
		WrappedKeyNonce:    wrappedKeyNonce,
		WrappedContentKey:  wrappedContentKey,
		ContentNonce:       contentNonce,
		Ciphertext:         ciphertext,
	}, nil
}

func DecryptForDevice(envelope Envelope, recipientPrivateKey, associatedData []byte) ([]byte, error) {
	if envelope.Version != EnvelopeVersion {
		return nil, ErrUnsupportedEnvelope
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(recipientPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse recipient private key: %w", err)
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(envelope.EphemeralPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse ephemeral public key: %w", err)
	}
	sharedSecret, err := privateKey.ECDH(ephemeral)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}

	wrappingKey := hkdfSHA256(sharedSecret, nil, []byte("nexdrop/private-transfer/v1"), contentKeySize)
	contentKey, err := open(wrappingKey, envelope.WrappedKeyNonce, envelope.WrappedContentKey, associatedData)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	plaintext, err := open(contentKey, envelope.ContentNonce, envelope.Ciphertext, associatedData)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return plaintext, nil
}

func seal(key, plaintext, associatedData []byte) ([]byte, []byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	return nonce, aead.Seal(nil, nonce, plaintext, associatedData), nil
}

func open(key, nonce, ciphertext, associatedData []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, ErrInvalidEnvelope
	}
	return aead.Open(nil, nonce, ciphertext, associatedData)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return aead, nil
}

func hkdfSHA256(secret, salt, info []byte, length int) []byte {
	if salt == nil {
		salt = make([]byte, sha256.Size)
	}
	extract := hmac.New(sha256.New, salt)
	_, _ = extract.Write(secret)
	pseudorandomKey := extract.Sum(nil)

	result := make([]byte, 0, length)
	var previous []byte
	for counter := byte(1); len(result) < length; counter++ {
		expand := hmac.New(sha256.New, pseudorandomKey)
		_, _ = expand.Write(previous)
		_, _ = expand.Write(info)
		_, _ = expand.Write([]byte{counter})
		previous = expand.Sum(nil)
		result = append(result, previous...)
	}
	return result[:length]
}
