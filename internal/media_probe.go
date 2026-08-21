package internal

import (
	"path/filepath"
	"strings"
)

func probeMediaFile(path, quality, container string) (resolvedContainer, resolution, codec string) {
	resolvedContainer = strings.ToLower(strings.TrimSpace(container))
	if resolvedContainer == "" {
		resolvedContainer = containerFromPath(path)
	}
	resolution = resolutionFromQuality(quality)
	codec = codecFromQuality(quality)
	if codec == "" {
		codec = codecFromPath(path)
	}
	return resolvedContainer, resolution, codec
}

func containerFromPath(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "mkv", "mp4", "avi", "mov", "m4v", "wmv", "ts", "m2ts":
		return ext
	default:
		return ext
	}
}

func resolutionFromQuality(quality string) string {
	q := strings.ToLower(quality)
	switch {
	case strings.Contains(q, "2160") || strings.Contains(q, "4k") || strings.Contains(q, "uhd"):
		return "2160p"
	case strings.Contains(q, "1080"):
		return "1080p"
	case strings.Contains(q, "720"):
		return "720p"
	case strings.Contains(q, "576"):
		return "576p"
	case strings.Contains(q, "480"):
		return "480p"
	default:
		return ""
	}
}

func codecFromQuality(quality string) string {
	q := strings.ToLower(quality)
	switch {
	case strings.Contains(q, "x265"), strings.Contains(q, "hevc"), strings.Contains(q, "h265"):
		return "hevc"
	case strings.Contains(q, "x264"), strings.Contains(q, "h264"), strings.Contains(q, "avc"):
		return "h264"
	case strings.Contains(q, "xvid"):
		return "xvid"
	case strings.Contains(q, "vp9"):
		return "vp9"
	case strings.Contains(q, "av1"):
		return "av1"
	default:
		return ""
	}
}

func codecFromPath(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(name, "x265"), strings.Contains(name, "hevc"), strings.Contains(name, "h265"):
		return "hevc"
	case strings.Contains(name, "x264"), strings.Contains(name, "h264"):
		return "h264"
	default:
		return ""
	}
}

func applyMediaProbe(ec *EvalContext, path, quality, container string) {
	if ec == nil {
		return
	}
	c, r, v := probeMediaFile(path, quality, container)
	ec.MediaContainer = c
	ec.VideoResolution = r
	ec.VideoCodec = v
}
