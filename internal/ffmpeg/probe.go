package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

type Probe struct {
	Path       string  `json:"path"`
	Duration   float64 `json:"duration"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	HasAudio   bool    `json:"hasAudio"`
	VideoCodec string  `json:"videoCodec"`
	AudioCodec string  `json:"audioCodec,omitempty"`
}

type ffprobeOut struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	CodecType    string `json:"codec_type"`
	CodecName    string `json:"codec_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	AvgFrameRate string `json:"avg_frame_rate"`
	RFrameRate   string `json:"r_frame_rate"`
	Duration     string `json:"duration"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

func ProbeFile(ctx context.Context, path string) (*Probe, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", wrapExecErr(err))
	}

	var parsed ffprobeOut
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse ffprobe: %w", err)
	}

	p := &Probe{Path: path, FPS: 60}
	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		p.Duration = d
	}

	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if p.Width != 0 {
				continue
			}
			p.Width = s.Width
			p.Height = s.Height
			p.VideoCodec = s.CodecName
			if fps := parseFPS(s.RFrameRate); fps > 0 {
				p.FPS = fps
			} else if fps := parseFPS(s.AvgFrameRate); fps > 0 {
				p.FPS = fps
			}
			p.FPS = snapFPS(p.FPS)
			if p.Duration == 0 {
				if d, err := strconv.ParseFloat(s.Duration, 64); err == nil {
					p.Duration = d
				}
			}
		case "audio":
			p.HasAudio = true
			p.AudioCodec = s.CodecName
		}
	}

	if p.Width == 0 || p.Height == 0 {
		return nil, fmt.Errorf("no video stream")
	}
	if p.Duration <= 0 {
		return nil, fmt.Errorf("unknown duration")
	}
	return p, nil
}

func parseFPS(r string) float64 {
	if r == "" || r == "0/0" {
		return 0
	}
	n, rest, ok := strings.Cut(r, "/")
	if !ok {
		v, _ := strconv.ParseFloat(r, 64)
		return v
	}
	a, err1 := strconv.ParseFloat(n, 64)
	b, err2 := strconv.ParseFloat(rest, 64)
	if err1 != nil || err2 != nil || b == 0 {
		return 0
	}
	return a / b
}

func snapFPS(fps float64) float64 {
	if fps <= 0 {
		return 60
	}
	for _, n := range []float64{24, 25, 30, 48, 50, 60, 72, 75, 90, 120, 144, 165, 240} {
		if math.Abs(fps-n) < 0.51 {
			return n
		}
	}
	return fps
}

func formatFPS(fps float64) string {
	fps = snapFPS(fps)
	if fps == math.Trunc(fps) && fps >= 1 {
		return strconv.FormatInt(int64(fps), 10)
	}
	return strconv.FormatFloat(fps, 'f', 3, 64)
}

func wrapExecErr(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
