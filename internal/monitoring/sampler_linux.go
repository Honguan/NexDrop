//go:build linux

package monitoring

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type systemSampler struct {
	mu               sync.Mutex
	previousCPUIdle  uint64
	previousCPUTotal uint64
	previousUpload   uint64
	previousDownload uint64
}

func NewSystemSampler() Sampler { return &systemSampler{} }

func (sampler *systemSampler) Sample(storagePath string) (Sample, error) {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	cpuIdle, cpuTotal, err := cpuCounters()
	if err != nil {
		return Sample{}, err
	}
	memory, err := usedMemoryBytes()
	if err != nil {
		return Sample{}, err
	}
	disk, err := usedDiskBytes(storagePath)
	if err != nil {
		return Sample{}, err
	}
	cache, err := cacheBytes(storagePath)
	if err != nil {
		return Sample{}, err
	}
	upload, download, err := networkCounters()
	if err != nil {
		return Sample{}, err
	}
	result := Sample{MemoryBytes: memory, DiskBytes: disk, CacheBytes: cache}
	if sampler.previousCPUTotal > 0 && cpuTotal > sampler.previousCPUTotal {
		totalDelta := cpuTotal - sampler.previousCPUTotal
		idleDelta := cpuIdle - sampler.previousCPUIdle
		result.CPUPercent = float32(totalDelta-idleDelta) * 100 / float32(totalDelta)
	}
	if sampler.previousUpload > 0 && upload >= sampler.previousUpload {
		result.NetworkUploadBytes = int64(upload - sampler.previousUpload)
	}
	if sampler.previousDownload > 0 && download >= sampler.previousDownload {
		result.NetworkDownloadBytes = int64(download - sampler.previousDownload)
	}
	sampler.previousCPUIdle, sampler.previousCPUTotal = cpuIdle, cpuTotal
	sampler.previousUpload, sampler.previousDownload = upload, download
	return result, nil
}

func cpuCounters() (uint64, uint64, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, 0, fmt.Errorf("read /proc/stat: %w", err)
		}
		return 0, 0, fmt.Errorf("empty /proc/stat")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 6 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("invalid /proc/stat cpu line")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	return values[3] + values[4], total, nil
}

func usedMemoryBytes() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	values := make(map[string]int64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "MemTotal:" || fields[0] == "MemAvailable:" {
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			values[fields[0]] = value * 1024
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if values["MemTotal:"] == 0 || values["MemAvailable:"] == 0 {
		return 0, fmt.Errorf("missing memory counters")
	}
	return values["MemTotal:"] - values["MemAvailable:"], nil
}

func usedDiskBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Blocks-stat.Bfree) * int64(stat.Bsize), nil
}

func networkCounters() (uint64, uint64, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var upload, download uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		separator := strings.IndexByte(line, ':')
		if separator < 0 {
			continue
		}
		fields := strings.Fields(line[separator+1:])
		if len(fields) < 16 {
			continue
		}
		received, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		transmitted, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		download += received
		upload += transmitted
	}
	return upload, download, scanner.Err()
}
