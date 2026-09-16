package ffmpeg

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// PathPoint and the path fields on Zoom remain for old sidecar compatibility.
// Automatic camera movement ignores them.
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

func lerpRect(a, b Rect, p float64) Rect {
	return Rect{
		X: lerp(a.X, b.X, p),
		Y: lerp(a.Y, b.Y, p),
		W: lerp(a.W, b.W, p),
		H: lerp(a.H, b.H, p),
	}
}

func cameraRectAt(zooms []Zoom, t float64) Rect {
	full := Rect{W: 1, H: 1}
	zs := make([]Zoom, 0, len(zooms))
	for _, z := range zooms {
		if z.valid() {
			zs = append(zs, z)
		}
	}
	if len(zs) == 0 {
		return full
	}
	sort.SliceStable(zs, func(i, j int) bool { return zs[i].InStart < zs[j].InStart })

	i := -1
	for n := range zs {
		if t < zs[n].InStart {
			break
		}
		i = n
	}
	if i < 0 {
		return full
	}

	z := zs[i]
	from := full
	if i > 0 {
		from = zs[i-1].Rect
	}
	if t < z.InEnd {
		d := math.Max(z.InEnd-z.InStart, 0.0001)
		return lerpRect(from, z.Rect, ease((t-z.InStart)/d, z.Easing))
	}
	if i == len(zs)-1 && t > z.OutStart {
		d := math.Max(z.OutEnd-z.OutStart, 0.0001)
		return lerpRect(z.Rect, full, ease((t-z.OutStart)/d, z.Easing))
	}
	return z.Rect
}

func CropAt(zooms []Zoom, t float64, w, h int) (x, y, cw, ch int) {
	return cameraRectAt(zooms, t).pixels(w, h)
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
		fps = 60
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
		fmt.Fprintf(&b, "%.6f crop@z w %d, crop@z h %d, crop@z x %d, crop@z y %d;\n", t, cw, ch, x, y)
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
		"settb=AVTB,setpts=PTS-STARTPTS,format=yuv444p,sendcmd=f='%s',crop@z=w=%d:h=%d:x=0:y=0:exact=1,scale=%d:%d:flags=lanczos+accurate_rnd+full_chroma_int,format=yuv420p,setsar=1",
		p, w, h, w, h,
	)
}

type Speedup struct {
	ID     string  `json:"id"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Factor float64 `json:"factor,omitempty"`
}

func (s Speedup) valid() bool {
	return s.End > s.Start && s.Factor >= 0.25 && s.Factor <= 4 && s.Factor != 1
}

func HasSpeedups(ss []Speedup) bool {
	for _, s := range ss {
		if s.valid() {
			return true
		}
	}
	return false
}

// BuildSetpts returns a setpts filter that compresses each speedup block on
// the source timeline. ponytail: overlapping blocks stack; the UI clamps
// instead of resolving overlaps here.
func BuildSetpts(ss []Speedup) string {
	terms := make([]string, 0, len(ss))
	for _, s := range ss {
		if !s.valid() {
			continue
		}
		k := 1 - 1/s.Factor
		terms = append(terms, fmt.Sprintf(
			"if(lt(T\\,%.4f)\\,0\\,(min(T\\,%.4f)-%.4f)*%.6f/TB)",
			s.Start, s.End, s.Start, k))
	}
	if len(terms) == 0 {
		return ""
	}
	return "setpts=PTS-" + strings.Join(terms, "-")
}

// OutputDuration is how long the exported video runs after speeding up blocks.
func OutputDuration(dur float64, ss []Speedup) float64 {
	out := dur
	for _, s := range ss {
		if s.valid() {
			out -= (s.End - s.Start) * (1 - 1/s.Factor)
		}
	}
	return math.Max(0, out)
}

func atempoChain(f float64) string {
	out := ""
	for f > 2 {
		out += "atempo=2,"
		f /= 2
	}
	for f < 0.5 {
		out += "atempo=0.5,"
		f *= 2
	}
	return out + "atempo=" + strconv.FormatFloat(f, 'f', -1, 64)
}

// BuildAtempo splits audio at speedup boundaries, respeeds each part and
// rejoins it, so audio stays in sync with the compressed video.
func BuildAtempo(ss []Speedup, duration float64) string {
	bounds := []float64{0, duration}
	for _, s := range ss {
		if s.valid() {
			bounds = append(bounds, s.Start, s.End)
		}
	}
	sort.Float64s(bounds)
	uniq := bounds[:0]
	for i, b := range bounds {
		if i == 0 || b > uniq[len(uniq)-1] {
			uniq = append(uniq, b)
		}
	}
	bounds = uniq
	n := len(bounds) - 1
	if n < 1 {
		return ""
	}
	if n == 1 {
		for _, s := range ss {
			if s.valid() && s.Start <= 0.001 && s.End >= duration-0.001 {
				return "asetpts=PTS-STARTPTS," + atempoChain(s.Factor)
			}
		}
		return "asetpts=PTS-STARTPTS"
	}
	parts := make([]string, 0, n+2)
	split := fmt.Sprintf("asetpts=PTS-STARTPTS,asplit=%d", n)
	for i := 0; i < n; i++ {
		split += fmt.Sprintf("[s%d]", i)
	}
	parts = append(parts, split)
	ins := make([]string, 0, n)
	for i := 0; i < n; i++ {
		a, b := bounds[i], bounds[i+1]
		f := 1.0
		for _, s := range ss {
			if s.valid() && s.Start <= a+0.001 && s.End >= b-0.001 {
				f = s.Factor
			}
		}
		seg := fmt.Sprintf("atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS", a, b)
		if f != 1 {
			seg += "," + atempoChain(f)
		}
		ins = append(ins, fmt.Sprintf("[a%d]", i))
		parts = append(parts, fmt.Sprintf("[s%d]%s[a%d]", i, seg, i))
	}
	parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=0:a=1", strings.Join(ins, ""), n))
	return strings.Join(parts, ";")
}
