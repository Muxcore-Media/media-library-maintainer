package internal

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"
)

type overlayStyle struct {
	BarColor      color.RGBA
	PillColor     color.RGBA
	PillTextColor color.RGBA
	DateFormat    string
	UseDays       bool
}

func (m *Module) overlayStyle() overlayStyle {
	m.cfgMu.RLock()
	dateFmt := m.overlayDateFormat
	useDays := m.overlayShowDate
	bar := m.overlayBarColor
	pill := m.overlayPillColor
	pillText := m.overlayPillTextColor
	m.cfgMu.RUnlock()

	style := overlayStyle{
		BarColor:      parseOverlayColor(bar, color.RGBA{R: 180, G: 30, B: 30, A: 210}),
		PillColor:     parseOverlayColor(pill, color.RGBA{R: 20, G: 20, B: 20, A: 220}),
		PillTextColor: parseOverlayColor(pillText, color.RGBA{R: 255, G: 220, B: 80, A: 255}),
		DateFormat:    dateFmt,
		UseDays:       useDays,
	}
	if style.DateFormat == "" {
		if v := os.Getenv("MAINTAINER_OVERLAY_DATE_FORMAT"); v != "" {
			style.DateFormat = v
		} else {
			style.DateFormat = "Jan 2"
		}
	}
	if !style.UseDays {
		style.UseDays = !strings.EqualFold(os.Getenv("MAINTAINER_OVERLAY_SHOW_DATE"), "false")
	}
	return style
}

func (s overlayStyle) BarColorString() string {
	return rgbaString(s.BarColor)
}

func (s overlayStyle) PillColorString() string {
	return rgbaString(s.PillColor)
}

func (s overlayStyle) PillTextColorString() string {
	return rgbaString(s.PillTextColor)
}

func rgbaString(c color.RGBA) string {
	return fmt.Sprintf("%d,%d,%d,%d", c.R, c.G, c.B, c.A)
}

func parseOverlayColor(raw string, fallback color.RGBA) color.RGBA {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if strings.HasPrefix(raw, "#") && len(raw) == 7 {
		r, _ := strconv.ParseUint(raw[1:3], 16, 8)
		g, _ := strconv.ParseUint(raw[3:5], 16, 8)
		b, _ := strconv.ParseUint(raw[5:7], 16, 8)
		return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
	}
	parts := strings.Split(raw, ",")
	if len(parts) >= 3 {
		r, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		g, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		b, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		a := 255
		if len(parts) >= 4 {
			a, _ = strconv.Atoi(strings.TrimSpace(parts[3]))
		}
		return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}
	}
	return fallback
}
