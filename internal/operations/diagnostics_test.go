package operations

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"
)

func TestWriteDiagnosticsRedactsKnownSecrets(t *testing.T) {
	const password = "Password!ShouldNeverAppear"
	token := strings.Join([]string{
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`)),
		"secret",
		"signature",
	}, ".")
	const privateKey = "-----BEGIN PRIVATE KEY-----\nprivate-material\n-----END PRIVATE KEY-----"
	const totpSecret = "JBSWY3DPEHPK3PXP"
	const nodeKey = "node-private-key-material"
	const messageContent = "confidential-message-content"
	const plaintextFilename = "private-tax-return.pdf"
	var output bytes.Buffer

	err := WriteDiagnostics(context.Background(), &output, DiagnosticsOptions{
		Version:     "2.2.0",
		Commit:      "abcdef1",
		GeneratedAt: time.Date(2026, 7, 30, 1, 2, 3, 0, time.UTC),
		Checks: []Check{{
			Name: "database",
			OK:   false,
			Detail: "postgres://nexdrop:" + password +
				"@postgres:5432/nexdrop?access_token=" + token,
		}},
		Environment: map[string]string{
			"NEXDROP_DOMAIN":                      "node.example.com",
			"NEXDROP_DATABASE_PASSWORD":           password,
			"NEXDROP_ACCESS_TOKEN":                token,
			"NEXDROP_TLS_PRIVATE_KEY":             privateKey,
			"NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET": totpSecret,
			"NEXDROP_NODE_KEY":                    nodeKey,
			"MESSAGE_CONTENT":                     messageContent,
			"PLAINTEXT_FILENAME":                  plaintextFilename,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte(password)) || bytes.Contains(output.Bytes(), []byte(token)) {
		t.Fatal("diagnostics archive contains a known secret")
	}

	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) < 3 {
		t.Fatalf("diagnostics entries = %d", len(reader.File))
	}
	var combined strings.Builder
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(entry)
		_ = entry.Close()
		if err != nil {
			t.Fatal(err)
		}
		combined.Write(content)
	}
	content := combined.String()
	if !strings.Contains(content, "node.example.com") || !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("diagnostics content = %s", content)
	}
	for _, forbidden := range []string{password, token, privateKey, totpSecret, nodeKey, messageContent, plaintextFilename} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("decompressed diagnostics contains %q: %s", forbidden, content)
		}
	}
}

func TestRedactTextRemovesCredentialsAndBearerTokens(t *testing.T) {
	input := "postgres://user:p%40ss@postgres:5432/nexdrop Authorization: Bearer abc.def.ghi"
	redacted := RedactText(input, nil)
	if strings.Contains(redacted, "p%40ss") || strings.Contains(redacted, "abc.def.ghi") {
		t.Fatalf("redacted text = %q", redacted)
	}
}
