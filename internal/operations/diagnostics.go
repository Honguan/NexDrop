package operations

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type DiagnosticsOptions struct {
	Version     string
	Commit      string
	GeneratedAt time.Time
	Checks      []Check
	Environment map[string]string
}

var (
	credentialURLPattern  = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/@\s]+:)[^@\s]+(@)`)
	bearerPattern         = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	querySecretPattern    = regexp.MustCompile(`(?i)([?&](?:access_token|token|secret|password|key)=)[^&\s]+`)
	assignedSecretPattern = regexp.MustCompile(`(?i)((?:password|token|secret|authorization|cookie|private_key|node_key)\s*[=:]\s*)(?:bearer\s+)?[^,;\s]+`)
)

func WriteDiagnostics(_ context.Context, writer io.Writer, options DiagnosticsOptions) error {
	if options.GeneratedAt.IsZero() {
		options.GeneratedAt = time.Now().UTC()
	}
	secrets := knownSecrets(options.Environment)
	checks := append([]Check(nil), options.Checks...)
	for index := range checks {
		checks[index].Detail = RedactText(checks[index].Detail, secrets)
	}
	configuration := redactEnvironment(options.Environment)
	entries := []struct {
		name  string
		value any
	}{
		{"manifest.json", map[string]any{
			"schemaVersion":  1,
			"generatedAt":    options.GeneratedAt.UTC(),
			"productVersion": options.Version,
			"buildCommit":    options.Commit,
		}},
		{"health.json", map[string]any{"healthy": Healthy(checks), "checks": checks}},
		{"configuration.json", configuration},
		{"system.json", map[string]any{
			"goVersion": runtime.Version(),
			"goos":      runtime.GOOS,
			"goarch":    runtime.GOARCH,
			"cpus":      runtime.NumCPU(),
		}},
	}

	archive := zip.NewWriter(writer)
	for _, entry := range entries {
		file, err := archive.Create(entry.name)
		if err != nil {
			_ = archive.Close()
			return err
		}
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(entry.value); err != nil {
			_ = archive.Close()
			return err
		}
	}
	return archive.Close()
}

func RedactText(value string, secrets []string) string {
	result := credentialURLPattern.ReplaceAllString(value, `${1}[REDACTED]${2}`)
	result = bearerPattern.ReplaceAllString(result, `${1}[REDACTED]`)
	result = querySecretPattern.ReplaceAllString(result, `${1}[REDACTED]`)
	result = assignedSecretPattern.ReplaceAllString(result, `${1}[REDACTED]`)
	for _, secret := range secrets {
		if secret != "" {
			result = strings.ReplaceAll(result, secret, "[REDACTED]")
		}
	}
	return result
}

func knownSecrets(environment map[string]string) []string {
	secrets := make([]string, 0)
	for key, value := range environment {
		if isSensitiveKey(key) && value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}

func redactEnvironment(environment map[string]string) map[string]string {
	result := make(map[string]string, len(environment))
	for key, value := range environment {
		if isSensitiveKey(key) {
			result[key] = "[REDACTED]"
			continue
		}
		if _, ok := diagnosticConfigurationKeys[key]; !ok {
			continue
		}
		result[key] = RedactText(value, knownSecrets(environment))
	}
	return result
}

var diagnosticConfigurationKeys = map[string]struct{}{
	"NEXDROP_DOMAIN":                        {},
	"NEXDROP_HTTP_ADDRESS":                  {},
	"NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE":   {},
	"NEXDROP_PAIRING_RATE_LIMIT_PER_MINUTE": {},
	"NEXDROP_ADMIN_RATE_LIMIT_PER_MINUTE":   {},
	"NEXDROP_STORAGE_WARNING_PERCENT":       {},
	"NEXDROP_STORAGE_STOP_PERCENT":          {},
	"NEXDROP_PROTOCOL_VERSION":              {},
	"NEXDROP_MINIMUM_CLIENT_VERSION":        {},
}

func isSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"PASSWORD", "TOKEN", "SECRET", "KEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
