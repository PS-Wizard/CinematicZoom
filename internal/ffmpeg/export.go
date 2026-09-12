package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Encoder struct {
	Name string
	Args []string
}

func DetectEncoder() Encoder {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	s := ""
	if err == nil {
		s = string(out)
	}
	switch {
	case strings.Contains(s, "h264_nvenc"):
		return Encoder{
			Name: "h264_nvenc",
			Args: []string{
				"-c:v", "h264_nvenc",
				"-preset", "p5",
				"-tune", "hq",
				"-rc", "constqp",
				"-qp", "16",
				"-bf", "0",
				"-spatial-aq", "0",
				"-pix_fmt", "yuv420p",
			},
		}
	case strings.Contains(s, "h264_amf"):
		return Encoder{
			Name: "h264_amf",
			Args: []string{
				"-c:v", "h264_amf",
				"-quality", "quality",
				"-pix_fmt", "yuv420p",
			},
		}
	default:
		return Encoder{
			Name: "libx264",
			Args: []string{
				"-c:v", "libx264",
				"-crf", "16",
				"-preset", "fast",
				"-tune", "animation",
				"-bf", "0",
				"-pix_fmt", "yuv420p",
			},
		}
	}
}

type ProgressFunc func(ratio float64, timeSec float64, line string)

type ExportOpts struct {
	Source  string
	Output  string
	Probe   *Probe
	Zooms   []Zoom
	Encoder Encoder
}

func Export(ctx context.Context, opts ExportOpts, progress ProgressFunc) error {
	if opts.Probe == nil {
		return fmt.Errorf("missing probe")
	}
	if opts.Encoder.Name == "" {
		opts.Encoder = DetectEncoder()
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-progress", "pipe:1",
		"-nostats",
		"-i", opts.Source,
	}

	if !HasZooms(opts.Zooms) {
		args = append(args, "-c", "copy", "-avoid_negative_ts", "make_zero")
	} else {
		fps := snapFPS(opts.Probe.FPS)
		cmdFile, err := os.CreateTemp("", "cinematic-*.cmd")
		if err != nil {
			return fmt.Errorf("sendcmd file: %w", err)
		}
		defer os.Remove(cmdFile.Name())
		if _, err := cmdFile.WriteString(BuildSendCmd(opts.Zooms, opts.Probe.Width, opts.Probe.Height, fps, opts.Probe.Duration)); err != nil {
			cmdFile.Close()
			return fmt.Errorf("sendcmd write: %w", err)
		}
		if err := cmdFile.Close(); err != nil {
			return err
		}
		filter := BuildVideoFilter(cmdFile.Name(), opts.Probe.Width, opts.Probe.Height, fps)
		args = append(args, "-vf", filter, "-r", formatFPS(fps), "-fps_mode", "cfr")
		args = append(args, opts.Encoder.Args...)
		if opts.Probe.HasAudio {
			if copyAudio(opts.Probe.AudioCodec) {
				args = append(args, "-c:a", "copy")
			} else {
				args = append(args, "-c:a", "aac", "-b:a", "192k")
			}
		} else {
			args = append(args, "-an")
		}
	}
	args = append(args, "-movflags", "+faststart", opts.Output)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	var logBuf strings.Builder
	logsDone := make(chan struct{})
	go func() {
		defer close(logsDone)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			logBuf.WriteString(line)
			logBuf.WriteByte('\n')
			if usefulLog(line) && progress != nil {
				progress(-1, -1, line)
			}
		}
	}()

	duration := opts.Probe.Duration
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lastT float64
	for sc.Scan() {
		line := sc.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "out_time_us":
			us, err := strconv.ParseFloat(v, 64)
			if err == nil && us >= 0 {
				lastT = us / 1e6
				ratio := 0.0
				if duration > 0 {
					ratio = lastT / duration
					if ratio > 1 {
						ratio = 1
					}
				}
				if progress != nil {
					progress(ratio, lastT, "")
				}
			}
		case "progress":
			if v == "end" && progress != nil {
				progress(1, duration, "")
			}
		}
	}

	waitErr := cmd.Wait()
	<-logsDone
	if waitErr != nil {
		tail := strings.TrimSpace(logBuf.String())
		if tail != "" {
			return fmt.Errorf("ffmpeg: %w\n%s", waitErr, tail)
		}
		return fmt.Errorf("ffmpeg: %w", wrapExecErr(waitErr))
	}
	return nil
}

func copyAudio(codec string) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp3", "opus", "ac3", "eac3":
		return true
	default:
		return false
	}
}

func usefulLog(line string) bool {
	l := strings.ToLower(line)
	if strings.Contains(l, "error") || strings.Contains(l, "warning") {
		return true
	}
	return strings.HasPrefix(line, "frame=") || strings.Contains(line, "Stream mapping")
}
