package ffmpeg

// Preset is a named export quality choice: encoder args plus an optional
// output downscale.
type Preset struct {
	Name    string
	Encoder Encoder
	ScaleW  int
	ScaleH  int
}

// PresetNames lists the choices in UI order.
var PresetNames = []string{"default", "smooth", "quality", "small", "tiny"}

// ResolvePreset maps a preset name to encoder settings. Unknown or empty
// names fall back to the fast hardware default.
func ResolvePreset(name string) Preset {
	x264 := func(crf, speed string) Encoder {
		return Encoder{Name: "libx264", Args: []string{
			"-c:v", "libx264",
			"-crf", crf,
			"-preset", speed,
			"-tune", "animation",
			"-bf", "0",
			"-pix_fmt", "yuv420p",
		}}
	}
	switch name {
	case "smooth":
		return Preset{Name: name, Encoder: x264("12", "fast")}
	case "quality":
		return Preset{Name: name, Encoder: x264("14", "slow")}
	case "small":
		return Preset{Name: name, ScaleW: 1600, ScaleH: 1000, Encoder: x264("26", "slow")}
	case "tiny":
		return Preset{Name: name, ScaleW: 1280, ScaleH: 800, Encoder: x264("30", "slow")}
	default:
		return Preset{Name: "default", Encoder: DetectEncoder()}
	}
}
