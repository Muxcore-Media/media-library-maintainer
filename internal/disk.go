package internal

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func diskFreePercent(path string) float64 {
	if path == "" {
		return 100
	}
	path = filepath.Clean(path)
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 100
	}
	total := float64(stat.Blocks) * float64(stat.Bsize)
	if total <= 0 {
		return 100
	}
	free := float64(stat.Bavail) * float64(stat.Bsize)
	return free / total * 100
}

func diskFreeBytes(path string) int64 {
	if path == "" {
		return 0
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(filepath.Clean(path), &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}

// parseSizeThreshold parses Deleterr-style sizes: 100GB, 1TB, 500MB.
func parseSizeThreshold(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty threshold")
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "TB"):
		mult = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		mult = 1024
		s = strings.TrimSuffix(s, "KB")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return int64(n * float64(mult)), nil
}

func (m *Module) diskGateAllowsAction(rootFolder string) bool {
	maxFree := m.getDiskActMaxFreePercent()
	if maxFree <= 0 {
		return true
	}
	return diskFreePercent(rootFolder) <= maxFree
}

func (m *Module) freeUpTargetMet(rootFolder string, targetPercent float64) bool {
	if targetPercent <= 0 {
		return false
	}
	return diskFreePercent(rootFolder) >= targetPercent
}
