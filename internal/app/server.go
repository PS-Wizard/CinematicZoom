package app

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cinematic/internal/ffmpeg"
	"cinematic/web"
)

type Server struct {
	home    string
	encoder ffmpeg.Encoder
	mu      sync.Mutex
	busy    bool
}

func Run(addr string, openBrowser bool) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return fmt.Errorf("ffprobe not found in PATH")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		return err
	}

	s := &Server{
		home:    home,
		encoder: ffmpeg.DetectEncoder(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/fs", s.handleFS)
	mux.HandleFunc("GET /api/probe", s.handleProbe)
	mux.HandleFunc("GET /api/project", s.handleGetProject)
	mux.HandleFunc("PUT /api/project", s.handlePutProject)
	mux.HandleFunc("GET /media", s.handleMedia)
	mux.HandleFunc("POST /api/export", s.handleExport)
	mux.Handle("/", http.FileServer(http.FS(static)))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	url := "http://" + ln.Addr().String()
	log.Printf("cinematic on %s  encoder=%s", url, s.encoder.Name)

	if openBrowser {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = exec.Command("xdg-open", url).Start()
		}()
	}

	return http.Serve(ln, mux)
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	videos := filepath.Join(s.home, "Videos")
	start := s.home
	if st, err := os.Stat(videos); err == nil && st.IsDir() {
		start = videos
	}
	writeJSON(w, map[string]any{
		"home":    s.home,
		"start":   start,
		"encoder": s.encoder.Name,
	})
}

type fsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = s.home
	}
	abs, err := s.cleanPath(p)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	if !st.IsDir() {
		httpError(w, http.StatusBadRequest, fmt.Errorf("not a directory"))
		return
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		httpError(w, http.StatusForbidden, err)
		return
	}
	out := make([]fsEntry, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(abs, name)
		if e.IsDir() {
			out = append(out, fsEntry{Name: name, Path: full, Dir: true})
			continue
		}
		if !isVideo(name) {
			continue
		}
		out = append(out, fsEntry{Name: name, Path: full, Dir: false, Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	parent := filepath.Dir(abs)
	if parent == abs {
		parent = ""
	}
	writeJSON(w, map[string]any{
		"path":    abs,
		"parent":  parent,
		"entries": out,
	})
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	p, err := s.cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	info, err := ffmpeg.ProbeFile(r.Context(), p)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	data, err := os.ReadFile(sidecarPath(p))
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"source": p, "zooms": []any{}, "speeds": []any{}, "cropStart": 0, "cropEnd": 0})
			return
		}
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

type projectFile struct {
	Source    string           `json:"source"`
	Zooms     []ffmpeg.Zoom    `json:"zooms"`
	Speeds    []ffmpeg.Speedup `json:"speeds"`
	CropStart float64          `json:"cropStart,omitempty"`
	CropEnd   float64          `json:"cropEnd,omitempty"`
}

func (s *Server) handlePutProject(w http.ResponseWriter, r *http.Request) {
	var proj projectFile
	if err := json.NewDecoder(r.Body).Decode(&proj); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	src, err := s.cleanPath(proj.Source)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	proj.Source = src
	data, err := json.MarshalIndent(proj, "", "  ")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(sidecarPath(src), data, 0o644); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": sidecarPath(src)})
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	p, err := s.cleanPath(r.URL.Query().Get("path"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	st, err := os.Stat(p)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	if st.IsDir() {
		httpError(w, http.StatusBadRequest, fmt.Errorf("not a file"))
		return
	}
	http.ServeFile(w, r, p)
}

type exportReq struct {
	Source    string           `json:"source"`
	Output    string           `json:"output"`
	Zooms     []ffmpeg.Zoom    `json:"zooms"`
	Speeds    []ffmpeg.Speedup `json:"speeds"`
	CropStart float64          `json:"cropStart"`
	CropEnd   float64          `json:"cropEnd"`
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		httpError(w, http.StatusConflict, fmt.Errorf("export already running"))
		return
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	var req exportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	src, err := s.cleanPath(req.Source)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.cleanPath(req.Output)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if src == out {
		httpError(w, http.StatusBadRequest, fmt.Errorf("output cannot overwrite the source"))
		return
	}
	if filepath.Ext(strings.ToLower(out)) == "" {
		out += ".mp4"
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}

	probe, err := ffmpeg.ProbeFile(r.Context(), src)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	send := func(v any) {
		_ = enc.Encode(v)
		if flusher != nil {
			flusher.Flush()
		}
	}

	send(map[string]any{
		"type":    "start",
		"encoder": s.encoder.Name,
		"output":  out,
	})

	err = ffmpeg.Export(r.Context(), ffmpeg.ExportOpts{
		Source:    src,
		Output:    out,
		Probe:     probe,
		Zooms:     req.Zooms,
		Speedups:  req.Speeds,
		CropStart: req.CropStart,
		CropEnd:   req.CropEnd,
		Encoder:   s.encoder,
	}, func(ratio, timeSec float64, line string) {
		if line != "" {
			send(map[string]any{"type": "log", "line": line})
			return
		}
		send(map[string]any{"type": "progress", "ratio": ratio, "time": timeSec})
	})
	if err != nil {
		send(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	send(map[string]any{"type": "done", "output": out})
}

func (s *Server) cleanPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path required")
	}
	if strings.HasPrefix(p, "~"+string(os.PathSeparator)) || p == "~" {
		p = filepath.Join(s.home, strings.TrimPrefix(p, "~"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func sidecarPath(video string) string {
	ext := filepath.Ext(video)
	return strings.TrimSuffix(video, ext) + ".cinematic.json"
}

func isVideo(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".webm", ".mov", ".m4v", ".avi":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func httpError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
