package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildVideoFilterEmpty(t *testing.T) {
	if HasZooms(nil) {
		t.Fatal("empty should have no zooms")
	}
}

func TestBuildSendCmdHasCropCommands(t *testing.T) {
	z := []Zoom{{
		ID:       "z1",
		InStart:  1,
		InEnd:    1.5,
		OutStart: 3,
		OutEnd:   3.5,
		Rect:     Rect{X: 0.25, Y: 0.25, W: 0.5, H: 0.5},
		Easing:   "easeInOutCubic",
	}}
	got := BuildSendCmd(z, 1920, 1080, 30, 4)
	for _, want := range []string{"crop@z w", "crop@z h", "crop@z x", "crop@z y"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	x, y, w, h := CropAt(z, 2.0, 1920, 1080)
	if w != 960 || h != 540 {
		t.Fatalf("hold crop got %d,%d %dx%d want 960x540", x, y, w, h)
	}
	if x != 480 || y != 270 {
		t.Fatalf("hold origin %d,%d", x, y)
	}
	x, y, w, h = CropAt(z, 0.2, 1920, 1080)
	if x != 0 || y != 0 || w != 1920 || h != 1080 {
		t.Fatalf("outside zoom should be full frame, got %d,%d %dx%d", x, y, w, h)
	}
}

func TestCropAtPansDuringHold(t *testing.T) {
	z := []Zoom{{
		ID:       "z1",
		InStart:  1,
		InEnd:    1.5,
		OutStart: 3,
		OutEnd:   3.5,
		Rect:     Rect{X: 0, Y: 0.25, W: 0.5, H: 0.5},
		Path: []PathPoint{
			{T: 1.5, X: 0, Y: 0.25, W: 0.5, H: 0.5},
			{T: 3.0, X: 0.5, Y: 0.25, W: 0.5, H: 0.5},
		},
		Easing: "linear",
	}}
	x1, _, w1, _ := CropAt(z, 1.5, 1920, 1080)
	x2, _, w2, _ := CropAt(z, 3.0, 1920, 1080)
	if w1 != 960 || w2 != 960 {
		t.Fatalf("hold size %d %d want 960", w1, w2)
	}
	if x1 != 0 {
		t.Fatalf("start pan x=%d want 0", x1)
	}
	if x2 != 960 {
		t.Fatalf("end pan x=%d want 960", x2)
	}
	mid, _, _, _ := CropAt(z, 2.25, 1920, 1080)
	if mid != 480 {
		t.Fatalf("mid pan x=%d want 480", mid)
	}
	x0, y0, w0, h0 := CropAt(z, 0.2, 1920, 1080)
	if x0 != 0 || y0 != 0 || w0 != 1920 || h0 != 1080 {
		t.Fatalf("outside zoom should be full frame, got %d,%d %dx%d", x0, y0, w0, h0)
	}
}

func TestRectAtUsesPanEasing(t *testing.T) {
	z := Zoom{
		InStart:  1,
		InEnd:    1.5,
		OutStart: 3,
		OutEnd:   3.5,
		Rect:     Rect{X: 0, Y: 0.25, W: 0.5, H: 0.5},
		Path: []PathPoint{
			{T: 1.5, X: 0, Y: 0.25, W: 0.5, H: 0.5},
			{T: 3.0, X: 0.5, Y: 0.25, W: 0.5, H: 0.5},
		},
		Easing:    "linear",
		PanEasing: "easeOutCubic",
	}
	mid := rectAt(z, 2.25)
	if mid.X <= 0.26 || mid.X >= 0.99 {
		t.Fatalf("pan ease-out should lead linear 0.25, got x=%v", mid.X)
	}
}

func TestRectAtUsesZoomRectUntilPanStarts(t *testing.T) {
	z := Zoom{
		InStart:  1,
		InEnd:    1.5,
		OutStart: 4,
		OutEnd:   4.5,
		Rect:     Rect{X: 0.1, Y: 0.1, W: 0.5, H: 0.5},
		Path: []PathPoint{
			{T: 2.5, X: 0.4, Y: 0.1, W: 0.5, H: 0.5},
			{T: 3.5, X: 0.4, Y: 0.4, W: 0.5, H: 0.5},
		},
	}
	r := rectAt(z, 1.8)
	if r.X != 0.1 {
		t.Fatalf("before pan should keep zoom rect, got %+v", r)
	}
	r = rectAt(z, 2.5)
	if r.X != 0.4 {
		t.Fatalf("at pan start %+v", r)
	}
}

func TestRectAtHoldsInPlaceUntilMove(t *testing.T) {
	z := Zoom{
		InStart:  1,
		InEnd:    1.5,
		OutStart: 5,
		OutEnd:   5.5,
		Rect:     Rect{X: 0.1, Y: 0.2, W: 0.5, H: 0.5},
		Path: []PathPoint{
			{T: 1.5, X: 0.1, Y: 0.2, W: 0.5, H: 0.5},
			{T: 3.0, X: 0.1, Y: 0.2, W: 0.5, H: 0.5},
			{T: 4.0, X: 0.4, Y: 0.2, W: 0.5, H: 0.5},
		},
		PanEasing: "linear",
	}
	hold := rectAt(z, 2.2)
	if hold.X != 0.1 {
		t.Fatalf("still hold should stay at 0.1, got %+v", hold)
	}
	mid := rectAt(z, 3.5)
	if mid.X < 0.24 || mid.X > 0.26 {
		t.Fatalf("pan after hold should be halfway 0.25, got x=%v", mid.X)
	}
}

func TestRectAtFallsBackToStaticRect(t *testing.T) {
	z := Zoom{
		InStart:  1,
		InEnd:    1.5,
		OutStart: 3,
		OutEnd:   3.5,
		Rect:     Rect{X: 0.25, Y: 0.25, W: 0.5, H: 0.5},
	}
	r := rectAt(z, 2)
	if r.X != 0.25 || r.W != 0.5 {
		t.Fatalf("fallback rect %+v", r)
	}
}

func TestRectPixelsEvenSize(t *testing.T) {
	x, y, w, h := (Rect{X: 0.33, Y: 0.21, W: 0.4, H: 0.4}).pixels(1920, 1080)
	if w%2 != 0 || h%2 != 0 {
		t.Fatalf("odd crop size %d,%d %dx%d", x, y, w, h)
	}
}

func TestBuildVideoFilterLocksFPS(t *testing.T) {
	got := BuildVideoFilter("/tmp/x.cmd", 1920, 1080, 60)
	for _, want := range []string{"fps=fps=60", "format=yuv444p", "full_chroma_int", "format=yuv420p"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestSnapFPSNearSixty(t *testing.T) {
	if snapFPS(59.94) != 60 {
		t.Fatalf("59.94 should snap to 60, got %v", snapFPS(59.94))
	}
	if snapFPS(0) != 60 {
		t.Fatalf("0 should default to 60")
	}
	if formatFPS(60.0) != "60" {
		t.Fatalf("formatFPS 60 got %q", formatFPS(60.0))
	}
}

func TestExportZoomSmoke(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "smptebars=size=1280x720:rate=30:duration=4",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make source: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	probe, err := ProbeFile(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	err = Export(ctx, ExportOpts{
		Source: src,
		Output: dst,
		Probe:  probe,
		Zooms: []Zoom{{
			ID:       "z1",
			InStart:  0.4,
			InEnd:    0.9,
			OutStart: 2.4,
			OutEnd:   2.9,
			Rect:     Rect{X: 0.25, Y: 0.25, W: 0.5, H: 0.5},
			Easing:   "easeInOutCubic",
		}},
		Encoder: DetectEncoder(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 1000 {
		t.Fatalf("tiny output: %d", st.Size())
	}
	outProbe, err := ProbeFile(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if outProbe.Width != 1280 || outProbe.Height != 720 {
		t.Fatalf("size %dx%d", outProbe.Width, outProbe.Height)
	}

	full := filepath.Join(dir, "full.png")
	zoomed := filepath.Join(dir, "zoomed.png")
	extract := func(tsec, dest string) {
		t.Helper()
		c := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
			"-ss", tsec, "-i", dst, "-frames:v", "1", dest)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("extract %s: %v\n%s", dest, err, out)
		}
	}
	extract("0.1", full)
	extract("1.5", zoomed)
	a, _ := os.ReadFile(full)
	b, _ := os.ReadFile(zoomed)
	if string(a) == string(b) {
		t.Fatal("zoomed hold frame is identical to full frame — crop did not apply")
	}
}
