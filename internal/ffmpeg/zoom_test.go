package ffmpeg

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestCameraRectAtAutomaticallyMovesBetweenZooms(t *testing.T) {
	zooms := []Zoom{
		{
			ID: "a", InStart: 1, InEnd: 2, OutStart: 4, OutEnd: 4.5,
			Rect: Rect{X: 0, Y: 0.25, W: 0.5, H: 0.5}, Easing: "linear",
		},
		{
			ID: "b", InStart: 4, InEnd: 5, OutStart: 7, OutEnd: 8,
			Rect: Rect{X: 0.5, Y: 0.25, W: 0.5, H: 0.5}, Easing: "linear",
		},
	}
	tests := []struct {
		name string
		t    float64
		want Rect
	}{
		{"before first", 0, Rect{W: 1, H: 1}},
		{"first transition", 1.5, Rect{X: 0, Y: 0.125, W: 0.75, H: 0.75}},
		{"hold first", 3, zooms[0].Rect},
		{"move between points", 4.5, Rect{X: 0.25, Y: 0.25, W: 0.5, H: 0.5}},
		{"hold second", 6, zooms[1].Rect},
		{"final zoom out", 7.5, Rect{X: 0.25, Y: 0.125, W: 0.75, H: 0.75}},
		{"after final", 9, Rect{W: 1, H: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cameraRectAt(zooms, tt.t)
			if math.Abs(got.X-tt.want.X) > 0.001 || math.Abs(got.Y-tt.want.Y) > 0.001 ||
				math.Abs(got.W-tt.want.W) > 0.001 || math.Abs(got.H-tt.want.H) > 0.001 {
				t.Fatalf("cameraRectAt(%v) = %+v, want %+v", tt.t, got, tt.want)
			}
		})
	}
}

func TestRectPixelsEvenSize(t *testing.T) {
	x, y, w, h := (Rect{X: 0.33, Y: 0.21, W: 0.4, H: 0.4}).pixels(1920, 1080)
	if w%2 != 0 || h%2 != 0 {
		t.Fatalf("odd crop size %d,%d %dx%d", x, y, w, h)
	}
}

func TestBuildVideoFilterPreservesSourceTiming(t *testing.T) {
	got := BuildVideoFilter("/tmp/x.cmd", 1920, 1080)
	for _, want := range []string{"settb=AVTB", "setpts=PTS-STARTPTS", "format=yuv444p", "full_chroma_int", "format=yuv420p"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "fps=") {
		t.Fatalf("filter must preserve source timestamps: %s", got)
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

func TestBuildSetptsAndDuration(t *testing.T) {
	got := BuildSetpts([]Speedup{{ID: "s1", Start: 2, End: 4, Factor: 2}})
	want := "setpts=PTS-if(lt(T\\,2.0000)\\,0\\,(min(T\\,4.0000)-2.0000)*0.500000/TB)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if BuildSetpts(nil) != "" {
		t.Fatal("no speedups should give empty expr")
	}
	if d := OutputDuration(10, []Speedup{{Start: 2, End: 4, Factor: 2}}); d != 9 {
		t.Fatalf("duration %v want 9", d)
	}
	if c := atempoChain(0.25); c != "atempo=0.5,atempo=0.5" {
		t.Fatalf("atempoChain(0.25) = %q", c)
	}
	if got := BuildAtempo([]Speedup{{Start: 0, End: 4, Factor: 2}}, 4); got != "asetpts=PTS-STARTPTS,atempo=2" {
		t.Fatalf("full-range atempo = %q", got)
	}
}

func TestExportSpeedSmoke(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=6",
		"-map", "1:v", "-map", "0:a", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", src)
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
		Source: src, Output: dst, Probe: probe,
		Speedups: []Speedup{{ID: "s1", Start: 1, End: 5, Factor: 2}},
		Encoder:  DetectEncoder(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outProbe, err := ProbeFile(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if outProbe.Duration < 3.8 || outProbe.Duration > 4.4 {
		t.Fatalf("duration %v want ~4 (6s source, 4s block at 2x)", outProbe.Duration)
	}
}

func TestExportCropSmoke(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-f", "lavfi", "-i", "color=c=red:size=320x240:rate=30:duration=1.5[r];color=c=blue:size=320x240:rate=30:duration=4.5[b];[r][b]concat=n=2:v=1:a=0",
		"-map", "1:v", "-map", "0:a", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", src)
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
		Source: src, Output: dst, Probe: probe, CropStart: 1.5, CropEnd: 4.5, Encoder: DetectEncoder(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outProbe, err := ProbeFile(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if outProbe.Duration < 2.8 || outProbe.Duration > 3.3 {
		t.Fatalf("duration %v want ~3", outProbe.Duration)
	}
	pixel, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-ss", "0.1", "-i", dst,
		"-vf", "scale=1:1", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(pixel) < 3 || pixel[2] <= pixel[0] {
		t.Fatalf("crop did not start in the blue section: RGB %v", pixel)
	}
}

func TestExportPreservesVFRFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "vfr.mp4")
	dst := filepath.Join(dir, "out.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=60:duration=4",
		"-vf", "select=not(eq(mod(n\\,17)\\,0))", "-fps_mode", "vfr",
		"-c:v", "libx264", "-bf", "0", "-pix_fmt", "yuv420p", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make VFR source: %v\n%s", err, out)
	}
	frameTimes := func(path string) []float64 {
		t.Helper()
		out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
			"-show_entries", "frame=best_effort_timestamp_time", "-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var times []float64
		for _, line := range strings.Fields(string(out)) {
			tm, err := strconv.ParseFloat(strings.TrimSuffix(line, ","), 64)
			if err != nil {
				t.Fatal(err)
			}
			times = append(times, tm)
		}
		return times
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	probe, err := ProbeFile(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if probe.FPS >= 59 {
		t.Fatalf("probe used peak frame rate instead of average: %v", probe.FPS)
	}
	if probe.PeakFPS < 59 {
		t.Fatalf("peak frame rate not retained for camera sampling: %v", probe.PeakFPS)
	}
	err = Export(ctx, ExportOpts{
		Source: src, Output: dst, Probe: probe,
		Zooms:   []Zoom{{ID: "z", InStart: 0.2, InEnd: 0.7, OutStart: 3.2, OutEnd: 3.7, Rect: Rect{X: 0.25, Y: 0.25, W: 0.5, H: 0.5}}},
		Encoder: Encoder{Name: "libx264", Args: []string{"-c:v", "libx264", "-crf", "18", "-preset", "ultrafast", "-bf", "0", "-pix_fmt", "yuv420p"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	inTimes, outTimes := frameTimes(src), frameTimes(dst)
	if len(inTimes) != len(outTimes) {
		t.Fatalf("frame count changed from %d to %d", len(inTimes), len(outTimes))
	}
	for i := range inTimes {
		inPTS := inTimes[i] - inTimes[0]
		outPTS := outTimes[i] - outTimes[0]
		if math.Abs(inPTS-outPTS) > 0.001 {
			t.Fatalf("frame %d PTS changed from %.6f to %.6f", i, inPTS, outPTS)
		}
	}
}

func TestExportKeepsAVSyncWithLateStreams(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mkv")
	dst := filepath.Join(dir, "out.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-filter_complex", "[0:v]setpts=PTS+2/TB[v];[1:a]asetpts=PTS+2/TB[a]",
		"-map", "[v]", "-map", "[a]", "-copyts", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make delayed source: %v\n%s", err, out)
	}
	startTimes := func(path string) []float64 {
		t.Helper()
		out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
			"-show_entries", "stream=start_time", "-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var times []float64
		for _, line := range strings.Fields(string(out)) {
			if line == "N/A" {
				continue
			}
			tm, err := strconv.ParseFloat(line, 64)
			if err != nil {
				t.Fatal(err)
			}
			times = append(times, tm)
		}
		return times
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	probe, err := ProbeFile(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(startTimes(src)) != 1 {
		t.Skip("source did not keep late streams")
	}
	err = Export(ctx, ExportOpts{
		Source: src, Output: dst, Probe: probe,
		Zooms:   []Zoom{{ID: "z", InStart: 0.3, InEnd: 0.8, OutStart: 1.5, OutEnd: 2, Rect: Rect{X: 0.25, Y: 0.25, W: 0.5, H: 0.5}}},
		Encoder: DetectEncoder(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outTimes := startTimes(dst)
	if len(outTimes) == 0 || math.Abs(outTimes[0]) > 0.02 {
		t.Fatalf("video start time moved: %v", outTimes)
	}
}

func TestCropRangeFallbacks(t *testing.T) {
	if s, e, trim := cropRange(6, 0, 0); trim || s != 0 || e != 6 {
		t.Fatalf("unset crop = %v,%v trim %v", s, e, trim)
	}
	if s, e, _ := cropRange(6, 1, 10); s != 1 || e != 6 {
		t.Fatalf("late end = %v,%v", s, e)
	}
	if s, e, trim := cropRange(6, 10, 12); trim || s != 0 || e != 6 {
		t.Fatalf("past-end start = %v,%v trim %v", s, e, trim)
	}
	if s, e, trim := cropRange(6, 1.5, 4.5); !trim || s != 1.5 || e != 4.5 {
		t.Fatalf("normal crop = %v,%v trim %v", s, e, trim)
	}
}
