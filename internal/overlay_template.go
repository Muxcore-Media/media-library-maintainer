package internal

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type overlayTemplate struct {
	Name          string           `json:"name"`
	Mode          string           `json:"mode"`
	CanvasWidth   int              `json:"canvasWidth"`
	CanvasHeight  int              `json:"canvasHeight"`
	Elements      []overlayElement `json:"elements"`
}

type overlayElement struct {
	Type         string             `json:"type"`
	X            float64            `json:"x"`
	Y            float64            `json:"y"`
	Width        float64            `json:"width"`
	Height       float64            `json:"height"`
	LayerOrder   int                `json:"layerOrder"`
	Opacity      float64            `json:"opacity"`
	Visible      bool               `json:"visible"`
	ShapeType    string             `json:"shapeType"`
	FillColor    string             `json:"fillColor"`
	StrokeColor  *string            `json:"strokeColor"`
	StrokeWidth  float64            `json:"strokeWidth"`
	CornerRadius float64            `json:"cornerRadius"`
	Text         string             `json:"text"`
	Segments     []overlaySegment   `json:"segments"`
	FontSize     float64            `json:"fontSize"`
	FontColor    string             `json:"fontColor"`
	TextAlign    string             `json:"textAlign"`
	Uppercase    bool               `json:"uppercase"`
	DateFormat   string             `json:"dateFormat"`
	TextToday    string             `json:"textToday"`
	TextDay      string             `json:"textDay"`
	TextDays     string             `json:"textDays"`
	ImagePath    string             `json:"imagePath"`
}

type overlaySegment struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Field string `json:"field"`
}

type overlayRenderContext struct {
	ActAfter string
	Label    string
	Style    overlayStyle
}

const defaultPosterOverlayTemplate = `{
  "name":"Countdown Bar",
  "mode":"poster",
  "canvasWidth":1000,
  "canvasHeight":1500,
  "elements":[
    {"type":"shape","x":0,"y":1430,"width":1000,"height":70,"layerOrder":0,"opacity":0.85,"visible":true,"shapeType":"rectangle","fillColor":"#000000","cornerRadius":0},
    {"type":"variable","x":0,"y":1430,"width":1000,"height":70,"layerOrder":1,"opacity":1,"visible":true,"segments":[{"type":"variable","field":"daysText"}],"fontSize":38,"fontColor":"#FFFFFF","textAlign":"center","uppercase":true,"textToday":"TODAY","textDay":"LEAVING IN 1 DAY","textDays":"LEAVING IN {0} DAYS"}
  ]
}`

const defaultTitleCardOverlayTemplate = `{
  "name":"Title Card Pill",
  "mode":"titlecard",
  "canvasWidth":1920,
  "canvasHeight":1080,
  "elements":[
    {"type":"shape","x":40,"y":40,"width":480,"height":70,"layerOrder":0,"opacity":1,"visible":true,"shapeType":"rectangle","fillColor":"#B20710","cornerRadius":35},
    {"type":"variable","x":40,"y":40,"width":480,"height":70,"layerOrder":1,"opacity":1,"visible":true,"segments":[{"type":"text","value":"Leaving "},{"type":"variable","field":"date"}],"fontSize":38,"fontColor":"#FFFFFF","textAlign":"center","dateFormat":"Jan 2"}
  ]
}`

func (m *Module) overlayTemplateForMode(mode string) *overlayTemplate {
	raw := ""
	m.cfgMu.RLock()
	switch mode {
	case "titlecard":
		raw = m.overlayTitleCardTemplateJSON
	default:
		raw = m.overlayTemplateJSON
	}
	m.cfgMu.RUnlock()
	if raw == "" {
		if v := os.Getenv("MAINTAINER_OVERLAY_TEMPLATE_JSON"); v != "" && mode != "titlecard" {
			raw = v
		}
	}
	if raw == "" {
		if mode == "titlecard" {
			raw = defaultTitleCardOverlayTemplate
		} else {
			raw = defaultPosterOverlayTemplate
		}
	}
	var tpl overlayTemplate
	if json.Unmarshal([]byte(raw), &tpl) != nil {
		return nil
	}
	if tpl.CanvasWidth <= 0 {
		if mode == "titlecard" {
			tpl.CanvasWidth = 1920
			tpl.CanvasHeight = 1080
		} else {
			tpl.CanvasWidth = 1000
			tpl.CanvasHeight = 1500
		}
	}
	return &tpl
}

func renderOverlayTemplate(src image.Image, tpl *overlayTemplate, ctx overlayRenderContext) image.Image {
	if tpl == nil {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	sx := float64(b.Dx()) / float64(tpl.CanvasWidth)
	sy := float64(b.Dy()) / float64(tpl.CanvasHeight)
	elements := append([]overlayElement(nil), tpl.Elements...)
	sort.SliceStable(elements, func(i, j int) bool {
		return elements[i].LayerOrder < elements[j].LayerOrder
	})
	for _, el := range elements {
		if !el.Visible {
			continue
		}
		x := int(el.X * sx)
		y := int(el.Y * sy)
		w := int(el.Width * sx)
		h := int(el.Height * sy)
		if w <= 0 || h <= 0 {
			continue
		}
		rect := image.Rect(x, y, x+w, y+h).Intersect(b)
		if rect.Empty() {
			continue
		}
		switch el.Type {
		case "shape":
			drawOverlayShape(dst, rect, el)
		case "text", "variable":
			text := overlayElementText(el, ctx)
			if text == "" {
				continue
			}
			drawOverlayElementText(dst, rect, text, el, sx)
		case "image":
			if img := loadOverlayAsset(el.ImagePath); img != nil {
				scaled := resizeOverlayImage(img, rect.Dx(), rect.Dy())
				if scaled != nil {
					draw.Draw(dst, rect, scaled, image.Point{}, draw.Over)
				}
			}
		}
	}
	return dst
}

func drawOverlayShape(dst *image.RGBA, rect image.Rectangle, el overlayElement) {
	fill := parseOverlayColor(el.FillColor, color.RGBA{A: 255})
	if el.Opacity > 0 && el.Opacity < 1 {
		fill.A = uint8(float64(fill.A) * el.Opacity)
	}
	if el.ShapeType == "ellipse" {
		drawOverlayEllipse(dst, rect, fill)
		return
	}
	draw.Draw(dst, rect, &image.Uniform{C: fill}, image.Point{}, draw.Over)
}

func drawOverlayEllipse(dst *image.RGBA, rect image.Rectangle, col color.RGBA) {
	cx := (rect.Min.X + rect.Max.X) / 2
	cy := (rect.Min.Y + rect.Max.Y) / 2
	rx := (rect.Max.X - rect.Min.X) / 2
	ry := (rect.Max.Y - rect.Min.Y) / 2
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			dx := float64(x-cx) / float64(maxInt(rx, 1))
			dy := float64(y-cy) / float64(maxInt(ry, 1))
			if dx*dx+dy*dy <= 1 {
				dst.Set(x, y, col)
			}
		}
	}
}

func overlayElementText(el overlayElement, ctx overlayRenderContext) string {
	var parts []string
	if el.Type == "text" {
		parts = append(parts, el.Text)
	} else {
		for _, seg := range el.Segments {
			switch seg.Type {
			case "text":
				parts = append(parts, seg.Value)
			case "variable":
				parts = append(parts, resolveOverlayVariable(seg.Field, el, ctx))
			}
		}
	}
	out := strings.Join(parts, "")
	if el.Uppercase {
		out = strings.ToUpper(out)
	}
	return out
}

func resolveOverlayVariable(field string, el overlayElement, ctx overlayRenderContext) string {
	actAfter := ctx.ActAfter
	t, err := time.Parse(time.RFC3339, actAfter)
	days := 0
	if err == nil {
		days = int(time.Until(t).Hours() / 24)
		if days < 0 {
			days = 0
		}
	}
	switch field {
	case "date":
		layout := el.DateFormat
		if layout == "" {
			layout = ctx.Style.DateFormat
		}
		if err != nil && layout != "" {
			return strings.ToUpper(t.Format(layout))
		}
		return ctx.Label
	case "days":
		return itoa(days)
	case "daysText":
		if days <= 0 {
			if el.TextToday != "" {
				return el.TextToday
			}
			return "TODAY"
		}
		if days == 1 {
			if el.TextDay != "" {
				return el.TextDay
			}
			return "IN 1 DAY"
		}
		tmpl := el.TextDays
		if tmpl == "" {
			tmpl = "IN {0} DAYS"
		}
		return strings.ReplaceAll(tmpl, "{0}", itoa(days))
	default:
		return ""
	}
}

func drawOverlayElementText(dst *image.RGBA, rect image.Rectangle, text string, el overlayElement, scale float64) {
	col := parseOverlayColor(el.FontColor, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	y := rect.Min.Y + rect.Dy()/2
	x := rect.Min.X + 8
	if el.TextAlign == "center" {
		x = rect.Min.X + rect.Dx()/2 - len(text)*3
	}
	drawOverlayText(dst, x, y, text, col)
}

func loadOverlayAsset(name string) image.Image {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	dir := os.Getenv("MAINTAINER_OVERLAY_IMAGE_DIR")
	if dir == "" {
		dir = filepath.Join("/var/lib/media-library-maintainer", "overlay-images")
	}
	path := filepath.Join(dir, filepath.Base(name))
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func resizeOverlayImage(src image.Image, w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := sb.Min.X + (x*sb.Dx())/maxInt(w, 1)
			sy := sb.Min.Y + (y*sb.Dy())/maxInt(h, 1)
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func applyTemplateOverlay(src image.Image, m *Module, mode, actAfter, label string, style overlayStyle) image.Image {
	tpl := m.overlayTemplateForMode(mode)
	if tpl == nil {
		return drawOverlayBanner(src, label, overlaySubtitle(actAfter, style), style)
	}
	return renderOverlayTemplate(src, tpl, overlayRenderContext{
		ActAfter: actAfter,
		Label:    label,
		Style:    style,
	})
}
