package internal

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	mediaadminv1 "github.com/Muxcore-Media/contracts-media-admin/gen/muxcore/media/admin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func (m *Module) applyLeavingSoonOverlay(ctx context.Context, ec EvalContext, label, actAfter string) {
	if !m.getOverlayEnabled() {
		return
	}
	switch ec.Scope {
	case ScopeMovie, ScopeSeries, ScopeEpisode:
	default:
		return
	}
	if m.hasOverlayState(ctx, ec.Scope, ec.ItemID) {
		return
	}
	artType, tplMode := overlayPrimaryArtwork(ec.Scope)
	origPath, err := m.fetchArtworkPath(ctx, ec, artType)
	if err != nil || origPath == "" {
		slog.Debug("overlay: no artwork", "item", ec.ItemID, "type", artType, "error", err)
		return
	}
	img, err := m.loadPosterImage(ctx, origPath)
	if err != nil {
		slog.Debug("overlay: load artwork", "item", ec.ItemID, "error", err)
		return
	}
	if label == "" {
		label = "Leaving Soon"
	}
	style := m.overlayStyle()
	overlaid := applyTemplateOverlay(img, m, tplMode, actAfter, label, style)
	if err := m.uploadArtworkOverlay(ctx, ec, overlaid, artType); err != nil {
		slog.Debug("overlay: upload artwork", "item", ec.ItemID, "type", artType, "error", err)
		return
	}
	origBackdrop := ""
	if ec.Scope == ScopeSeries && m.getOverlayTitleCardEnabled() {
		if backdropPath, err := m.fetchArtworkPath(ctx, ec, "backdrop"); err == nil && backdropPath != "" {
			if backdropImg, err := m.loadPosterImage(ctx, backdropPath); err == nil {
				titleCard := applyTemplateOverlay(backdropImg, m, "titlecard", actAfter, label, style)
				if err := m.uploadArtworkOverlay(ctx, ec, titleCard, "backdrop"); err == nil {
					origBackdrop = backdropPath
				} else {
					slog.Debug("overlay: upload backdrop", "item", ec.ItemID, "error", err)
				}
			}
		}
	}
	m.saveOverlayState(ctx, ec.Scope, ec.ItemID, origPath, origBackdrop, label)
}

func overlayPrimaryArtwork(scope MediaScope) (artType, tplMode string) {
	if scope == ScopeEpisode {
		return "still", "titlecard"
	}
	return "poster", "poster"
}

func (m *Module) restoreLeavingSoonOverlay(ctx context.Context, ec EvalContext) {
	st := m.loadOverlayState(ctx, ec.Scope, ec.ItemID)
	if st == nil {
		return
	}
	artType, _ := overlayPrimaryArtwork(ec.Scope)
	if st.OriginalPosterPath != "" {
		if img, err := m.loadPosterImage(ctx, st.OriginalPosterPath); err == nil {
			_ = m.uploadArtworkOverlay(ctx, ec, img, artType)
		}
	}
	if st.OriginalBackdropPath != "" {
		if img, err := m.loadPosterImage(ctx, st.OriginalBackdropPath); err == nil {
			_ = m.uploadArtworkOverlay(ctx, ec, img, "backdrop")
		}
	}
	m.deleteOverlayState(ctx, ec.Scope, ec.ItemID)
}

type overlayState struct {
	OriginalPosterPath   string
	OriginalBackdropPath string
	OverlayLabel         string
}

func (m *Module) hasOverlayState(ctx context.Context, scope MediaScope, itemID string) bool {
	return m.loadOverlayState(ctx, scope, itemID) != nil
}

func (m *Module) loadOverlayState(ctx context.Context, scope MediaScope, itemID string) *overlayState {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return nil
	}
	var orig, backdrop, label string
	err := db.QueryRowContext(ctx, `SELECT original_poster_path, COALESCE(original_backdrop_path,''), overlay_label FROM overlay_state WHERE scope = ? AND item_id = ?`, scope, itemID).Scan(&orig, &backdrop, &label)
	if err != nil {
		return nil
	}
	return &overlayState{OriginalPosterPath: orig, OriginalBackdropPath: backdrop, OverlayLabel: label}
}

func (m *Module) saveOverlayState(ctx context.Context, scope MediaScope, itemID, origPath, origBackdrop, label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx, `INSERT INTO overlay_state (scope, item_id, original_poster_path, original_backdrop_path, overlay_label, applied_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, item_id) DO UPDATE SET original_poster_path=excluded.original_poster_path, original_backdrop_path=excluded.original_backdrop_path, overlay_label=excluded.overlay_label, applied_at=excluded.applied_at`,
		scope, itemID, origPath, origBackdrop, label, nowRFC())
}

func (m *Module) deleteOverlayState(ctx context.Context, scope MediaScope, itemID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx, `DELETE FROM overlay_state WHERE scope = ? AND item_id = ?`, scope, itemID)
}

func (m *Module) fetchArtworkPath(ctx context.Context, ec EvalContext, artworkType string) (string, error) {
	types := []string{artworkType}
	if ec.Scope == ScopeEpisode {
		types = []string{"still", "thumb", "titlecard", artworkType}
	}
	var lastErr error
	for _, t := range types {
		path, err := m.fetchArtworkPathTyped(ctx, ec, t)
		if err == nil && path != "" {
			return path, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("%s not found", artworkType)
}

func (m *Module) fetchArtworkPathTyped(ctx context.Context, ec EvalContext, artworkType string) (string, error) {
	addr, err := m.adminAddrForScope(ctx, ec.Scope)
	if err != nil {
		return "", err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	cli := mediaadminv1.NewMediaAdminServiceClient(conn)
	resp, err := cli.ListArtwork(ctx, &mediaadminv1.ListArtworkRequest{Id: ec.ItemID})
	if err != nil {
		return "", err
	}
	for _, a := range resp.GetArtwork() {
		if a.GetType() == parseArtworkType(artworkType) && a.GetUrl() != "" {
			return a.GetUrl(), nil
		}
	}
	return "", fmt.Errorf("%s not found", artworkType)
}

func overlaySubtitle(actAfter string, style overlayStyle) string {
	if style.UseDays {
		return formatOverlaySubtitle(actAfter, true)
	}
	if actAfter != "" {
		return formatOverlaySubtitleDate(actAfter, style.DateFormat)
	}
	return ""
}

func (m *Module) adminAddrForScope(ctx context.Context, scope MediaScope) (string, error) {
	switch scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return "", err
		}
		return m.findCapabilityAddr(ctx, "media.library.movies")
	case ScopeEpisode, ScopeSeason:
		if err := m.ensureTV(ctx); err != nil {
			return "", err
		}
		return m.findCapabilityAddr(ctx, "media.library.tv")
	default:
		if err := m.ensureTV(ctx); err != nil {
			return "", err
		}
		return m.findCapabilityAddr(ctx, "media.library.tv")
	}
}

func (m *Module) loadPosterImage(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	return decodeImage(data)
}

func decodeImage(data []byte) (image.Image, error) {
	if img, err := png.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}
	return jpeg.Decode(bytes.NewReader(data))
}

func formatOverlaySubtitle(actAfter string, useDays bool) string {
	if actAfter == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, actAfter)
	if err != nil {
		return ""
	}
	if useDays {
		d := int(time.Until(t).Hours() / 24)
		if d <= 0 {
			return "TODAY"
		}
		if d == 1 {
			return "IN 1 DAY"
		}
		return fmt.Sprintf("IN %d DAYS", d)
	}
	return t.Format("Jan 2")
}

func formatOverlaySubtitleDate(actAfter, layout string) string {
	t, err := time.Parse(time.RFC3339, actAfter)
	if err != nil || layout == "" {
		return ""
	}
	return strings.ToUpper(t.Format(layout))
}

func drawOverlayBanner(src image.Image, label, subtitle string, style overlayStyle) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	barH := b.Dy() / 6
	if barH < 24 {
		barH = 24
	}
	bar := image.Rect(b.Min.X, b.Max.Y-barH, b.Max.X, b.Max.Y)
	draw.Draw(dst, bar, &image.Uniform{C: style.BarColor}, image.Point{}, draw.Src)
	if label == "" {
		label = "Leaving Soon"
	}
	drawOverlayText(dst, b.Min.X+8, b.Max.Y-barH/2, strings.ToUpper(label), color.White)
	if subtitle != "" {
		pillW := len(subtitle)*7 + 16
		if pillW > b.Dx()-8 {
			pillW = b.Dx() - 8
		}
		pill := image.Rect(b.Max.X-pillW-8, b.Min.Y+8, b.Max.X-8, b.Min.Y+28)
		draw.Draw(dst, pill, &image.Uniform{C: style.PillColor}, image.Point{}, draw.Src)
		drawOverlayText(dst, pill.Min.X+8, pill.Min.Y+14, subtitle, style.PillTextColor)
	}
	return dst
}

func drawOverlayText(dst *image.RGBA, x, y int, text string, col color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y+basicfont.Face7x13.Metrics().Ascent.Ceil()/2),
	}
	d.DrawString(text)
}

func (m *Module) uploadArtworkOverlay(ctx context.Context, ec EvalContext, img image.Image, artworkType string) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	addr, err := m.adminAddrForScope(ctx, ec.Scope)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	cli := mediaadminv1.NewMediaAdminServiceClient(conn)
	stream, err := cli.ReplaceArtwork(ctx)
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("maintainer-overlay-%s.png", artworkType)
	msgs := []string{ec.ItemID, artworkType, filename}
	for _, part := range msgs {
		var req *mediaadminv1.ReplaceArtworkRequest
		switch part {
		case ec.ItemID:
			req = &mediaadminv1.ReplaceArtworkRequest{Data: &mediaadminv1.ReplaceArtworkRequest_ItemId{ItemId: part}}
		case artworkType:
			req = &mediaadminv1.ReplaceArtworkRequest{Data: &mediaadminv1.ReplaceArtworkRequest_ArtworkType{ArtworkType: parseArtworkType(part)}}
		case filename:
			req = &mediaadminv1.ReplaceArtworkRequest{Data: &mediaadminv1.ReplaceArtworkRequest_Filename{Filename: part}}
		}
		if sendErr := stream.Send(req); sendErr != nil {
			return sendErr
		}
	}
	chunkSize := 64 * 1024
	data := buf.Bytes()
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if sendErr := stream.Send(&mediaadminv1.ReplaceArtworkRequest{
			Data: &mediaadminv1.ReplaceArtworkRequest_Chunk{Chunk: data[i:end]},
		}); sendErr != nil {
			return sendErr
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

func parseArtworkType(s string) mediaadminv1.ArtworkType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "poster":
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_POSTER
	case "background", "backdrop":
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_BACKGROUND
	case "still", "titlecard":
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_STILL
	case "thumb":
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_THUMB
	case "banner":
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_BANNER
	case "logo":
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_LOGO
	default:
		return mediaadminv1.ArtworkType_ARTWORK_TYPE_UNSPECIFIED
	}
}
