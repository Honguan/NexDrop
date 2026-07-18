package version

import (
	"strconv"
	"strings"
)

const (
	APIVersion           = "1"
	CurrentProtocol      = "1.1"
	PreviousProtocol     = "1.0"
	MinimumClientVersion = "1.0"
)

var (
	ProductVersion = "1.0.5"
	BuildCommit    = "development"
)

type Information struct {
	ProductVersion       string `json:"productVersion"`
	BuildCommit          string `json:"buildCommit"`
	APIVersion           string `json:"apiVersion"`
	ProtocolVersion      string `json:"protocolVersion"`
	PreviousProtocol     string `json:"previousProtocolVersion"`
	MinimumClientVersion string `json:"minimumClientVersion"`
}

func Current() Information {
	return Information{ProductVersion: ProductVersion, BuildCommit: BuildCommit, APIVersion: APIVersion, ProtocolVersion: CurrentProtocol, PreviousProtocol: PreviousProtocol, MinimumClientVersion: MinimumClientVersion}
}

func SupportedProtocol(value string) bool {
	return value == CurrentProtocol || value == PreviousProtocol || value == "1"
}

func SupportedClient(value string) bool {
	separator := strings.LastIndex(value, "-v")
	if separator < 1 || separator+2 >= len(value) {
		return false
	}
	major, minor, ok := parse(strings.TrimSpace(value[separator+2:]))
	return ok && major == 1 && minor >= 0 && minor <= 1
}

func parse(value string) (int, int, bool) {
	parts := strings.Split(value, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return 0, 0, false
	}
	minor := 0
	if len(parts) == 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil || minor < 0 {
			return 0, 0, false
		}
	}
	return major, minor, true
}
