package internal

import (
	"context"
	"strings"

	ffprobev1 "github.com/Muxcore-Media/media-ffprobe/proto/ffprobev1"
)

func (m *Module) enrichFileProbe(ctx context.Context, ec *EvalContext) {
	if ec == nil || ec.FilePath == "" {
		return
	}
	if err := m.ensureFFprobe(ctx); err != nil {
		return
	}
	m.mu.RLock()
	cli := m.ffprobeClient
	m.mu.RUnlock()
	if cli == nil {
		return
	}
	result := m.cachedProbe(ctx, cli, ec.FilePath)
	if result == nil {
		resp, err := cli.Analyze(ctx, &ffprobev1.AnalyzeRequest{FilePath: ec.FilePath})
		if err != nil || resp == nil || resp.GetError() != "" {
			return
		}
		result = resp
	}
	applyProbeResult(ec, result)
}

func (m *Module) cachedProbe(ctx context.Context, cli ffprobev1.AnalysisServiceClient, path string) *ffprobev1.AnalyzeResponse {
	resp, err := cli.GetCached(ctx, &ffprobev1.GetCachedRequest{FilePath: path})
	if err != nil || resp == nil || !resp.GetFound() {
		return nil
	}
	return resp.GetResult()
}

func applyProbeResult(ec *EvalContext, result *ffprobev1.AnalyzeResponse) {
	if ec == nil || result == nil {
		return
	}
	if c := strings.TrimSpace(result.GetContainer()); c != "" {
		ec.MediaContainer = c
	}
	if v := result.GetVideo(); v != nil {
		if codec := strings.TrimSpace(v.GetCodec()); codec != "" {
			ec.VideoCodec = codec
		}
		if label := strings.TrimSpace(v.GetResolutionLabel()); label != "" {
			ec.VideoResolution = label
		} else if v.GetHeight() > 0 {
			ec.VideoResolution = resolutionLabelFromHeight(int(v.GetHeight()))
		}
		if v.GetWidth() > 0 {
			ec.VideoWidth = int(v.GetWidth())
		}
		if v.GetHeight() > 0 {
			ec.VideoHeight = int(v.GetHeight())
		}
		if v.GetBitrate() > 0 {
			ec.VideoBitrateKbps = v.GetBitrate() / 1000
		} else if result.GetOverallBitrate() > 0 {
			ec.VideoBitrateKbps = result.GetOverallBitrate() / 1000
		}
		ec.VideoHDR = v.GetHdr()
		if pf := strings.TrimSpace(v.GetPixelFormat()); pf != "" {
			ec.VideoBitDepth = bitDepthFromPixelFormat(pf)
		}
	}
	if q := result.GetQuality(); q != nil {
		if ec.VideoResolution == "" {
			ec.VideoResolution = strings.TrimSpace(q.GetResolution())
		}
		if ec.VideoCodec == "" {
			ec.VideoCodec = strings.TrimSpace(q.GetCodecGroup())
		}
	}
	if len(result.GetAudio()) > 0 {
		a := result.GetAudio()[0]
		ch := int(a.GetChannels())
		if ch > ec.AudioChannels {
			ec.AudioChannels = ch
		}
		if codec := strings.TrimSpace(a.GetCodec()); codec != "" {
			ec.AudioCodec = codec
		}
	}
}

func bitDepthFromPixelFormat(pf string) int {
	pf = strings.ToLower(pf)
	switch {
	case strings.Contains(pf, "12"):
		return 12
	case strings.Contains(pf, "10"):
		return 10
	default:
		return 8
	}
}

func resolutionLabelFromHeight(height int) string {
	switch {
	case height >= 2160:
		return "2160p"
	case height >= 1080:
		return "1080p"
	case height >= 720:
		return "720p"
	case height >= 576:
		return "576p"
	case height > 0:
		return "480p"
	default:
		return ""
	}
}
