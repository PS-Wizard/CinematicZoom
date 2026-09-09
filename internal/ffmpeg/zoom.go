package ffmpeg

import (
	"fmt"
	"math"
	"strings"
)

type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Zoom struct {
	ID       string  `json:"id"`
	InStart  float64 `json:"inStart"`
	InEnd    float64 `json:"inEnd"`
	OutStart float64 `json:"outStart"`
	OutEnd   float64 `json:"outEnd"`
	Rect     Rect    `json:"rect"`
	Easing   string  `json:"easing"`
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
	x = even(int(math.Round(r.X * float64(w))))
	y = even(int(math.Round(r.Y * float64(h))))
	if x+cw > w {
		x = even(w - cw)
	}
	if y+ch > h {
		y = even(h - ch)
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
	if strings.EqualFold(kind, "linear") {
		return p
	}
	if p < 0.5 {
		return 4 * p * p * p
	}
	u := -2*p + 2
	return 1 - u*u*u/2
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

func CropAt(zooms []Zoom, t float64, w, h int) (x, y, cw, ch int) {
	for _, z := range zooms {
		if !z.valid() {
			continue
		}
		a := amountFor(z, t)
		if a <= 0 {
			continue
		}
		r := Rect{
			X: lerp(0, z.Rect.X, a),
			Y: lerp(0, z.Rect.Y, a),
			W: lerp(1, z.Rect.W, a),
			H: lerp(1, z.Rect.H, a),
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
	if fps <= 0 {
		fps = 30
	}
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

func BuildVideoFilter(cmdPath string, w, h int) string {
	p := strings.ReplaceAll(cmdPath, `\`, `\\`)
	p = strings.ReplaceAll(p, `'`, `\'`)
	p = strings.ReplaceAll(p, `:`, `\:`)
	return fmt.Sprintf(
		"sendcmd=f='%s',crop@z=w=%d:h=%d:x=0:y=0:exact=1,scale=%d:%d:flags=lanczos,setsar=1",
		p, w, h, w, h,
	)
}
