package lan

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"time"
)

type Identity struct {
	ShortDeviceID  string
	Certificate    tls.Certificate
	Fingerprint    string
	CertificatePEM string
}

type TrustDirectory interface {
	Fingerprint(shortDeviceID string) (string, bool)
}

type CertificateTrustDirectory interface {
	TrustDirectory
	CertificatePEM(shortDeviceID string) (string, bool)
}

type StaticTrust map[string]string

func (trust StaticTrust) Fingerprint(shortDeviceID string) (string, bool) {
	value, ok := trust[shortDeviceID]
	return value, ok
}

type StaticCertificateTrust map[string]Identity

func (trust StaticCertificateTrust) Fingerprint(shortDeviceID string) (string, bool) {
	identity, ok := trust[shortDeviceID]
	return identity.Fingerprint, ok
}

func (trust StaticCertificateTrust) CertificatePEM(shortDeviceID string) (string, bool) {
	identity, ok := trust[shortDeviceID]
	return identity.CertificatePEM, ok
}

func GenerateIdentity(shortDeviceID string, now time.Time) (Identity, error) {
	if len(shortDeviceID) < 6 || len(shortDeviceID) > 32 || !identifier(shortDeviceID) {
		return Identity{}, errors.New("invalid LAN identity")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return Identity{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "nexdrop:" + shortDeviceID},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"nexdrop-" + shortDeviceID + ".local"},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return Identity{}, err
	}
	leaf, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		return Identity{}, err
	}
	fingerprint := sha256.Sum256(certificateDER)
	return Identity{
		ShortDeviceID:  shortDeviceID,
		Certificate:    tls.Certificate{Certificate: [][]byte{certificateDER}, PrivateKey: privateKey, Leaf: leaf},
		Fingerprint:    hex.EncodeToString(fingerprint[:]),
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})),
	}, nil
}

func serverTLSConfig(identity Identity, trust TrustDirectory) *tls.Config {
	return &tls.Config{
		MinVersion:            tls.VersionTLS13,
		Certificates:          []tls.Certificate{identity.Certificate},
		ClientAuth:            tls.RequireAnyClientCert,
		VerifyPeerCertificate: verifyPeer(trust, ""),
	}
}

func clientTLSConfig(identity Identity, trust TrustDirectory, expectedDeviceID string) (*tls.Config, error) {
	certificateTrust, ok := trust.(CertificateTrustDirectory)
	if !ok {
		return nil, errors.New("trusted LAN certificate unavailable")
	}
	certificatePEM, ok := certificateTrust.CertificatePEM(expectedDeviceID)
	block, rest := pem.Decode([]byte(certificatePEM))
	if !ok || block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("invalid trusted LAN certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid trusted LAN certificate")
	}
	expectedFingerprint, ok := trust.Fingerprint(expectedDeviceID)
	actualFingerprint := sha256.Sum256(certificate.Raw)
	expectedBytes, decodeErr := hex.DecodeString(expectedFingerprint)
	if !ok || decodeErr != nil || len(expectedBytes) != sha256.Size || subtle.ConstantTimeCompare(expectedBytes, actualFingerprint[:]) != 1 {
		return nil, errors.New("LAN peer fingerprint mismatch")
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return &tls.Config{
		MinVersion:            tls.VersionTLS13,
		Certificates:          []tls.Certificate{identity.Certificate},
		RootCAs:               roots,
		ServerName:            "nexdrop-" + expectedDeviceID + ".local",
		VerifyPeerCertificate: verifyPeer(trust, expectedDeviceID),
	}, nil
}

func verifyPeer(trust TrustDirectory, expectedDeviceID string) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCertificates [][]byte, _ [][]*x509.Certificate) error {
		if trust == nil || len(rawCertificates) != 1 {
			return errors.New("untrusted LAN peer")
		}
		certificate, err := x509.ParseCertificate(rawCertificates[0])
		if err != nil || certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature) != nil || time.Now().Before(certificate.NotBefore) || time.Now().After(certificate.NotAfter) {
			return errors.New("invalid LAN peer certificate")
		}
		deviceID := strings.TrimPrefix(certificate.Subject.CommonName, "nexdrop:")
		if deviceID == certificate.Subject.CommonName || (expectedDeviceID != "" && deviceID != expectedDeviceID) {
			return errors.New("unexpected LAN peer identity")
		}
		expected, ok := trust.Fingerprint(deviceID)
		actual := sha256.Sum256(rawCertificates[0])
		expectedBytes, decodeErr := hex.DecodeString(expected)
		if !ok || decodeErr != nil || len(expectedBytes) != sha256.Size || subtle.ConstantTimeCompare(expectedBytes, actual[:]) != 1 {
			return errors.New("LAN peer fingerprint mismatch")
		}
		return nil
	}
}
