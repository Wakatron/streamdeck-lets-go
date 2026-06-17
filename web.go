package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"streamdeck-lets-go/internal/config"
)

//go:embed static/*
var staticFS embed.FS

var staticRoot fs.FS

func init() {
	var err error
	staticRoot, err = fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("static embed: %v", err))
	}
}

type WebServer struct {
	cfg        *config.Config
	configPath string
	pm         *PageManager
	extraPMs   []*PageManager
	decks      []*Deck
	mu         sync.RWMutex

	sseClients map[chan string]struct{}
	sseMu      sync.RWMutex
}

func NewWebServer(cfg *config.Config, configPath string) *WebServer {
	return &WebServer{
		cfg:        cfg,
		configPath: configPath,
		sseClients: make(map[chan string]struct{}),
	}
}

func (s *WebServer) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

func (s *WebServer) SetPageManager(pm *PageManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pm = pm
}

func (s *WebServer) SetDecks(decks []*Deck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decks = decks
}

func (s *WebServer) SetExtraPageManagers(pms []*PageManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extraPMs = pms
}

func (s *WebServer) BroadcastPageChange(page string) {
	s.sseMu.RLock()
	defer s.sseMu.RUnlock()
	for ch := range s.sseClients {
		select {
		case ch <- page:
		default:
		}
	}
}

func (s *WebServer) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("GET /api/config/download", s.handleDownloadConfig)
	mux.HandleFunc("POST /api/config/restore", s.handleRestoreConfig)

	mux.HandleFunc("GET /api/pages", s.handleGetPages)

	mux.HandleFunc("GET /api/render", s.handleRender)

	mux.HandleFunc("GET /api/display-outputs", s.handleDisplayOutputs)

	mux.HandleFunc("GET /api/backups", s.handleListBackups)
	mux.HandleFunc("GET /api/backups/{filename}", s.handleGetBackup)

	mux.HandleFunc("GET /api/models", s.handleGetModels)

	mux.HandleFunc("GET /api/decks", s.handleGetDecks)

	mux.HandleFunc("GET /api/status", s.handleGetStatus)

	mux.HandleFunc("GET /api/events", s.handleSSE)
	mux.HandleFunc("POST /api/activate-page", s.handleActivatePage)

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))))

	mux.HandleFunc("/", s.handleSPA)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/upload" {
			s.handleUpload(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	srv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down web server")
		srv.Close()
	}()

	slog.Info("web server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web server: %w", err)
	}
	return nil
}

func (s *WebServer) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := fs.ReadFile(staticRoot, "index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *WebServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 8)

	s.sseMu.Lock()
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()

	notify := r.Context().Done()
	go func() {
		<-notify
		s.sseMu.Lock()
		delete(s.sseClients, ch)
		s.sseMu.Unlock()
	}()

	flusher.Flush()

	for {
		select {
		case <-notify:
			return
		case page := <-ch:
			data, _ := json.Marshal(map[string]string{"page": page})
			fmt.Fprintf(w, "event: page_changed\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *WebServer) handleActivatePage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Page string `json:"page"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if req.Page == "" {
		http.Error(w, "page is required", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	allPMs := append([]*PageManager{s.pm}, s.extraPMs...)
	s.mu.RUnlock()

	for _, pm := range allPMs {
		if pm == nil {
			continue
		}
		if err := pm.ActivatePage(req.Page); err != nil {
			slog.Warn("activate page", "page", req.Page, "error", err)
		} else {
			pm.stopPeriodicKeys()
			pm.startPeriodicKeys()
		}
	}

	s.BroadcastPageChange(req.Page)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "page": req.Page})
}

func (s *WebServer) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.cfg)
}

func (s *WebServer) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if err := newCfg.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid config: %v", err), http.StatusBadRequest)
		return
	}

	path := config.ConfigPath(s.configPath)

	s.mu.Lock()
	oldCfg := s.cfg
	s.cfg = &newCfg
	s.mu.Unlock()

	if err := s.cfg.Save(s.configPath); err != nil {
		s.mu.Lock()
		s.cfg = oldCfg
		s.mu.Unlock()
		slog.Error("save config", "error", err)
		http.Error(w, fmt.Sprintf("save failed: %v", err), http.StatusInternalServerError)
		return
	}

	backupOldConfig(path)
	gcImages(&newCfg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (s *WebServer) handleDownloadConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		http.Error(w, "serialization error", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("streamdeck-config-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write(data)
}

func (s *WebServer) handleRestoreConfig(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	var newCfg config.Config

	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, fmt.Sprintf("parse form: %v", err), http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, fmt.Sprintf("file field 'file' required: %v", err), http.StatusBadRequest)
			return
		}
		defer file.Close()
		if err := json.NewDecoder(file).Decode(&newCfg); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
	}

	if err := newCfg.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid config: %v", err), http.StatusBadRequest)
		return
	}

	path := config.ConfigPath(s.configPath)
	backupOldConfig(path)

	s.mu.Lock()
	s.cfg = &newCfg
	s.mu.Unlock()

	if err := s.cfg.Save(s.configPath); err != nil {
		http.Error(w, fmt.Sprintf("save failed: %v", err), http.StatusInternalServerError)
		return
	}
	gcImages(&newCfg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restored"})
}

func (s *WebServer) handleGetPages(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type pageInfo struct {
		Name string     `json:"name"`
		Keys []config.KeyConfig `json:"keys"`
		Icon string     `json:"icon,omitempty"`
	}

	pages := make([]pageInfo, 0, len(s.cfg.Pages))
	for _, p := range s.cfg.Pages {
		pages = append(pages, pageInfo{
			Name: p.Name,
			Keys: p.Keys,
			Icon: p.Icon,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pages)
}

func (s *WebServer) handleRender(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	keySize := 72
	if s := q.Get("key_size"); s != "" {
		fmt.Sscanf(s, "%d", &keySize)
	}

	kc := &config.KeyConfig{}

	if page := q.Get("page"); page != "" {
		key := q.Get("key")
		s.mu.RLock()
		for _, p := range s.cfg.Pages {
			if p.Name == page {
				for _, k := range p.Keys {
					if fmt.Sprintf("%d", k.Index) == key {
						kc = &k
						break
					}
				}
				break
			}
		}
		s.mu.RUnlock()
	}

	if icon := q.Get("icon"); icon != "" {
		kc.Icon = icon
		if s := q.Get("icon_scale"); s != "" {
			v := 0.0
			fmt.Sscanf(s, "%f", &v)
			kc.IconScale = &v
		}
	}
	if label := q.Get("label"); label != "" {
		kc.Label = label
	}
	if fs := q.Get("font_size"); fs != "" {
		v := 0.0
		fmt.Sscanf(fs, "%f", &v)
		kc.FontSize = &v
	}
	if bg := q.Get("background"); bg != "" {
		kc.Background = bg
	}

	img := RenderKeyToImage(kc, keySize)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	png.Encode(w, img)
}

func (s *WebServer) handleDisplayOutputs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	pm := s.pm
	s.mu.RUnlock()

	if pm == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
		return
	}

	outputs := pm.GetDisplayOutputs()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(outputs)
}

func backupOldConfig(path string) {
	backupDir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		slog.Warn("backup: create dir", "error", err)
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	name := fmt.Sprintf("config.%s.json", time.Now().Format("2006-01-02T15-04-05"))
	dst := filepath.Join(backupDir, name)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		slog.Warn("backup: write", "error", err)
		return
	}

	entries, _ := os.ReadDir(backupDir)
	if len(entries) > 10 {
		sort.Slice(entries, func(i, j int) bool {
			fi, _ := entries[i].Info()
			fj, _ := entries[j].Info()
			return fi.ModTime().Before(fj.ModTime())
		})
		for _, e := range entries[:len(entries)-10] {
			os.Remove(filepath.Join(backupDir, e.Name()))
		}
	}
}

func gcImages(cfg *config.Config) {
	imgDir := filepath.Join(configDir(), "images")
	entries, err := os.ReadDir(imgDir)
	if err != nil {
		return
	}

	used := make(map[string]bool, len(entries))
	for _, p := range cfg.Pages {
		for _, k := range p.Keys {
			if k.Icon != "" {
				used[k.Icon] = true
			}
		}
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(imgDir, e.Name())
		if !used[path] {
			if err := os.Remove(path); err != nil {
				slog.Warn("gc: remove orphan image", "path", path, "error", err)
			} else {
				slog.Debug("gc: removed orphan image", "path", path)
			}
		}
	}
}

func (s *WebServer) handleListBackups(w http.ResponseWriter, r *http.Request) {
	path := config.ConfigPath(s.configPath)
	backupDir := filepath.Join(filepath.Dir(path), "backups")

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
		return
	}

	type backupInfo struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Time string `json:"time"`
	}

	var backups []backupInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "config.") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupInfo{
			Name: e.Name(),
			Size: fi.Size(),
			Time: fi.ModTime().Format(time.RFC3339),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Time > backups[j].Time
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(backups)
}

func (s *WebServer) handleGetBackup(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if filename == "" || strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	path := config.ConfigPath(s.configPath)
	backupDir := filepath.Join(filepath.Dir(path), "backups")
	fullPath := filepath.Join(backupDir, filename)

	if !strings.HasPrefix(fullPath, backupDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write(data)
}

func (s *WebServer) handleGetModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		PID    uint16 `json:"pid"`
		Name   string `json:"name"`
		KeysX  int    `json:"keys_x"`
		KeysY  int    `json:"keys_y"`
		KeySize int   `json:"key_size"`
	}

	models := make([]modelInfo, 0, len(knownDecks))
	for _, d := range knownDecks {
		models = append(models, modelInfo{
			PID:     d.PID,
			Name:    d.Name,
			KeysX:   d.KeysX,
			KeysY:   d.KeysY,
			KeySize: d.KeySize,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

func (s *WebServer) handleGetDecks(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]DeckInfo, 0, len(s.decks))
	for _, d := range s.decks {
		infos = append(infos, d.DeckInfo())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infos)
}

func (s *WebServer) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"config_version": s.cfg.Version,
		"default_page":   s.cfg.DefaultPage,
		"page_count":     len(s.cfg.Pages),
		"device_count":   len(s.cfg.Devices),
	})
}

func (s *WebServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, fmt.Sprintf("parse form: %v", err), http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("file field 'file' required: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploadDir := filepath.Join(configDir(), "images")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("create upload dir: %v", err), http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(handler.Filename)
	if ext == "" {
		ext = ".png"
	}
	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	savePath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(savePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("create file: %v", err), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, fmt.Sprintf("write file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"path": savePath})
}
