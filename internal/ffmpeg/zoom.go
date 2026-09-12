package ffmpeg

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type PathPoint struct {
	ID string  `json:"id,omitempty"`
	T  float64 `json:"t"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	W  float64 `json:"w"`
	H  float64 `json:"h"`
}

type Zoom struct {
	ID        string      `json:"id"`
	InStart   float64     `json:"inStart"`
	InEnd     float64     `json:"inEnd"`
	OutStart  float64     `json:"outStart"`
	OutEnd    float64     `json:"outEnd"`
	Rect      Rect        `json:"rect"`
	Path      []PathPoint `json:"path,omitempty"`
	Easing    string      `json:"easing"`
	PanEasing string      `json:"panEasing,omitempty"`
}

func (z Zoom) valid() bool {
	return z.OutEnd > z.InStart && z.Rect.W > 0 && z.Rect.H > 0
}

func (r Rect) clamp() Rect {
	r.W = clamp(r.W, 0.08, 1)
	r.H = clamp(r.H, 0.08, 1)
	r.X = clamp(r.X, 0, 1-r.W)
	r.Y = clamp(r.Y, 0, 1-r.H)
	return r
}

func (r Rect) pixels(w, h int) (x, y, cw, ch int) {
	r = r.clamp()
	cw = even(int(math.Round(r.W * float64(w))))
	ch = even(int(math.Round(r.H * float64(h))))
	if cw < 2 {
		cw = 2
	}
	if ch < 2 {
		ch = 2
	}
	x = int(math.Round(r.X * float64(w)))
	y = int(math.Round(r.Y * float64(h)))
	if x+cw > w {
		x = w - cw
	}
	if y+ch > h {
		y = h - ch
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}

func even(v int) int {
	if v < 0 {
		return 0
	}
	return v - v%2
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, v))
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

func ease(p float64, kind string) float64 {
	p = clamp(p, 0, 1)
	switch strings.ToLower(kind) {
	case "linear":
		return p
	case "easeincubic":
		return p * p * p
	case "easeoutcubic":
		u := 1 - p
		return 1 - u*u*u
	default:
		if p < 0.5 {
			return 4 * p * p * p
		}
		u := -2*p + 2
		return 1 - u*u*u/2
	}
}

func panEaseKind(z Zoom) string {
	if strings.TrimSpace(z.PanEasing) != "" {
		return z.PanEasing
	}
	if strings.TrimSpace(z.Easing) != "" {
		return z.Easing
	}
	return "easeInOutCubic"
}

func amountFor(z Zoom, t float64) float64 {
	if t <= z.InStart || t >= z.OutEnd {
		return 0
	}
	if t < z.InEnd {
		d := math.Max(z.InEnd-z.InStart, 0.0001)
		return ease((t-z.InStart)/d, z.Easing)
	}
	if t <= z.OutStart {
		return 1
	}
	d := math.Max(z.OutEnd-z.OutStart, 0.0001)
	return 1 - ease((t-z.OutStart)/d, z.Easing)
}

func pathPoints(z Zoom) []PathPoint {
	if len(z.Path) == 0 {
		return nil
	}
	pts := append([]PathPoint(nil), z.Path...)
	sort.Slice(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
	return pts
}

func rectAt(z Zoom, t float64) Rect {
	pts := pathPoints(z)
	if len(pts) == 0 {
		return z.Rect
	}
	first := pts[0]
	if t < first.T {
		return z.Rect
	}
	if t <= first.T || len(pts) == 1 {
		return Rect{X: first.X, Y: first.Y, W: first.W, H: first.H}
	}
	last := pts[len(pts)-1]
	if t >= last.T {
		return Rect{X: last.X, Y: last.Y, W: last.W, H: last.H}
	}
	for i := 1; i < len(pts); i++ {
		if t > pts[i].T {
			continue
		}
		a, b := pts[i-1], pts[i]
		d := math.Max(b.T-a.T, 0.0001)
		p := ease((t-a.T)/d, panEaseKind(z))
		return Rect{
			X: lerp(a.X, b.X, p),
			Y: lerp(a.Y, b.Y, p),
			W: lerp(a.W, b.W, p),
			H: lerp(a.H, b.H, p),
		}
	}
	return z.Rect
}

func CropAt(zooms []Zoom, t float64, w, h int) (x, y, cw, ch int) {
	for _, z := range zooms {
		if !z.valid() {
			continue
		}
		a := amountFor(z, t)
		if a <= 0 {
			continue
		}
		target := rectAt(z, t)
		r := Rect{
			X: lerp(0, target.X, a),
			Y: lerp(0, target.Y, a),
			W: lerp(1, target.W, a),
			H: lerp(1, target.H, a),
		}
		return r.pixels(w, h)
	}
	return 0, 0, even(w), even(h)
}

func HasZooms(zooms []Zoom) bool {
	for _, z := range zooms {
		if z.valid() {
			return true
		}
	}
	return false
}

func BuildSendCmd(zooms []Zoom, w, h int, fps, duration float64) string {
	fps = snapFPS(fps)
	if duration <= 0 {
		duration = 0
		for _, z := range zooms {
			if z.OutEnd > duration {
				duration = z.OutEnd
			}
		}
	}
	dt := 1 / fps
	var b strings.Builder
	prevW, prevH, prevX, prevY := -1, -1, -1, -1
	emit := func(t float64) {
		x, y, cw, ch := CropAt(zooms, t, w, h)
		if cw == prevW && ch == prevH && x == prevX && y == prevY {
			return
		}
		prevW, prevH, prevX, prevY = cw, ch, x, y
		fmt.Fprintf(&b, "%.4f crop@z w %d, crop@z h %d, crop@z x %d, crop@z y %d;\n", t, cw, ch, x, y)
	}
	emit(0)
	for t := dt; t <= duration+dt/2; t += dt {
		emit(t)
	}
	return b.String()
}

func BuildVideoFilter(cmdPath string, w, h int, fps float64) string {
	p := strings.ReplaceAll(cmdPath, `\`, `\\`)
	p = strings.ReplaceAll(p, `'`, `\'`)
	p = strings.ReplaceAll(p, `:`, `\:`)
	rate := formatFPS(fps)
	return fmt.Sprintf(
		"fps=fps=%s,format=yuv444p,sendcmd=f='%s',crop@z=w=%d:h=%d:x=0:y=0:exact=1,scale=%d:%d:flags=lanczos+accurate_rnd+full_chroma_int,format=yuv420p,setsar=1",
		rate, p, w, h, w, h,
	)
}
