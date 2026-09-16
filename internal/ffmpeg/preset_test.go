package ffmpeg

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResolvePreset(t *testing.T) {
	cases := map[string]struct {
		encoder string
		scale   bool
	}{
		"default": {"h264", false},
		"smooth":  {"libx264", false},
		"quality": {"libx264", false},
		"small":   {"libx264", true},
		"tiny":    {"libx264", true},
		"":        {"h264", false},
		"bogus":   {"h264", false},
	}
	for name, want := range cases {
		p := ResolvePreset(name)
		if name == "" || name == "default" || name == "bogus" {
			want.encoder = DetectEncoder().Name
		}
		if p.Encoder.Name != want.encoder {
			t.Errorf("%s: encoder %s want %s", name, p.Encoder.Name, want.encoder)
		}
		if want.scale && p.ScaleW == 0 {
			t.Errorf("%s: missing scale", name)
		}
		if !want.scale && p.ScaleW != 0 {
			t.Errorf("%s: unexpected scale %dx%d", name, p.ScaleW, p.ScaleH)
		}
	}
	for _, name := range PresetNames {
		p := ResolvePreset(name)
		if len(p.Encoder.Args) == 0 {
			t.Errorf("%s: no encoder args", name)
		}
	}
}

func TestExportScaledSmoke(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg missing")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp4")
	dst := filepath.Join(dir, "out.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=1920x1200:rate=30:duration=1",
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
	small := ResolvePreset("small")
	err = Export(ctx, ExportOpts{
		Source: src, Output: dst, Probe: probe,
		ScaleW: small.ScaleW, ScaleH: small.ScaleH, Encoder: small.Encoder,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ProbeFile(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 1600 || out.Height != 1000 {
		t.Fatalf("output %dx%d want 1600x1000", out.Width, out.Height)
	}
}
