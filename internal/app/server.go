package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"duoreader/internal/document"
	"duoreader/internal/reader"
	"duoreader/internal/ui"
)

type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	path string
}
type persisted struct {
	Last     string                  `json:"last"`
	Sessions map[string]reader.State `json:"sessions"`
	Settings reader.State            `json:"settings"`
}
type event struct {
	Kind  string       `json:"kind"`
	State reader.State `json:"state"`
}
type client struct {
	ch            chan event
	ready         uint64
	width, height int
}
type Server struct {
	mu         sync.Mutex
	state      reader.State
	book       *document.Book
	entries    []Entry
	clients    map[*client]bool
	store      persisted
	dataDir    string
	token      string
	listener   net.Listener
	http       *http.Server
	fullscreen func(bool)
}

func (s *Server) SetFullscreen(fn func(bool)) { s.mu.Lock(); s.fullscreen = fn; s.mu.Unlock() }

func New(booksDir, dataDir string) (*Server, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	s := &Server{state: reader.Default(), clients: map[*client]bool{}, dataDir: dataDir}
	s.store.Sessions = map[string]reader.State{}
	if data, err := os.ReadFile(filepath.Join(dataDir, "sessions.json")); err == nil {
		_ = json.Unmarshal(data, &s.store)
	}
	if s.store.Sessions == nil {
		s.store.Sessions = map[string]reader.State{}
	}
	if s.store.Settings.Mode != "" {
		s.state = s.store.Settings
		s.state.Document = ""
		s.state.Total = 0
		s.state.Page = 1
		s.state.Normalize()
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	s.token = hex.EncodeToString(secret)
	_ = filepath.WalkDir(booksDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if document.Supported(p) {
			st, err := d.Info()
			if err != nil {
				return nil
			}
			s.entries = append(s.entries, Entry{ID: fmt.Sprint(len(s.entries)), Name: d.Name(), Size: st.Size(), path: p})
		}
		return nil
	})
	sort.Slice(s.entries, func(i, j int) bool { return s.entries[i].Name < s.entries[j].Name })
	for i := range s.entries {
		s.entries[i].ID = fmt.Sprint(i)
	}
	return s, nil
}
func (s *Server) Start(addr string) (string, error) {
	l, err := net.Listen("tcp4", addr)
	if err != nil {
		return "", err
	}
	s.listener = l
	mux := http.NewServeMux()
	mux.HandleFunc("/api/library", s.library)
	mux.HandleFunc("/api/open", s.open)
	mux.HandleFunc("/api/upload", s.upload)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/action", s.action)
	mux.HandleFunc("/api/ready", s.ready)
	mux.HandleFunc("/api/book/", s.bookInfo)
	mux.HandleFunc("/document/", s.pdf)
	mux.HandleFunc("/resource/", s.resource)
	mux.Handle("/", http.FileServer(http.FS(ui.Files)))
	s.http = &http.Server{Handler: s.secure(mux), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() { _ = s.http.Serve(l) }()
	return "http://" + l.Addr().String() + "/?token=" + s.token, nil
}
func (s *Server) Close() {
	s.mu.Lock()
	s.saveLocked()
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s.http != nil {
		_ = s.http.Shutdown(ctx)
	}
}
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.listener.Addr().String() {
			http.Error(w, "host", 403)
			return
		}
		c, _ := r.Cookie("duoreader")
		valid := c != nil && c.Value == s.token
		if r.URL.Path == "/" && r.URL.Query().Get("token") == s.token {
			valid = true
			http.SetCookie(w, &http.Cookie{Name: "duoreader", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		}
		if !valid {
			http.Error(w, "Сессия недоступна", 403)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != "http://"+r.Host {
				http.Error(w, "origin", 403)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self' blob:; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasSuffix(r.URL.Path, ".mjs") {
			w.Header().Set("Content-Type", "text/javascript")
		}
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func post(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return false
	}
	return true
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if !post(w, r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "неверный запрос", 400)
		return false
	}
	return true
}
func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.entries
	if entries == nil {
		entries = []Entry{}
	}
	writeJSON(w, entries)
}
func (s *Server) OpenPath(p string) error {
	b, err := document.Open(p)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveLocked()
	old := s.state
	s.book = b
	next, ok := s.store.Sessions[b.ID]
	if !ok {
		next = reader.Default()
		next.Mode = old.Mode
		next.Swap = old.Swap
		next.Theme = old.Theme
		next.Font = old.Font
		next.Cover = old.Cover
		next.RTL = old.RTL
	}
	next.Document = b.ID
	next.Revision = old.Revision
	next.Normalize()
	s.state = next
	s.store.Last = p
	s.publishLocked()
	s.saveLocked()
	return nil
}
func (s *Server) Restore() {
	if s.store.Last != "" {
		_ = s.OpenPath(s.store.Last)
	}
}
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &q) {
		return
	}
	var p string
	s.mu.Lock()
	for _, e := range s.entries {
		if e.ID == q.ID {
			p = e.path
		}
	}
	s.mu.Unlock()
	if p == "" {
		http.Error(w, "нет книги", 404)
		return
	}
	if err := s.OpenPath(p); err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (1<<30)+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "нет файла", 400)
		return
	}
	defer f.Close()
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if !document.Supported(name) {
		http.Error(w, "неподдерживаемый формат", 400)
		return
	}
	dir, err := os.MkdirTemp(s.dataDir, "import-")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	p := filepath.Join(dir, name)
	out, err := os.Create(p)
	if err != nil {
		_ = os.Remove(dir)
		http.Error(w, err.Error(), 500)
		return
	}
	_, err = io.Copy(out, f)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.OpenPath(p)
	}
	if err != nil {
		_ = os.Remove(p)
		_ = os.Remove(dir)
		http.Error(w, err.Error(), 422)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) current(id string) *document.Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.book != nil && s.book.ID == id {
		return s.book
	}
	return nil
}
func (s *Server) bookInfo(w http.ResponseWriter, r *http.Request) {
	b := s.current(strings.TrimPrefix(r.URL.Path, "/api/book/"))
	if b == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, b)
}
func (s *Server) pdf(w http.ResponseWriter, r *http.Request) {
	b := s.current(strings.TrimPrefix(r.URL.Path, "/document/"))
	if b == nil || b.Format != "pdf" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeFile(w, r, b.Path)
}
func (s *Server) resource(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/resource/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	b := s.current(parts[0])
	if b == nil {
		http.NotFound(w, r)
		return
	}
	data, ok := b.Resources[parts[1]]
	if !ok {
		http.NotFound(w, r)
		return
	}
	typ := mime.TypeByExtension(filepath.Ext(parts[1]))
	if typ == "" {
		typ = http.DetectContentType(data)
	}
	if !strings.HasPrefix(typ, "image/") && !strings.HasPrefix(typ, "font/") {
		http.Error(w, "тип ресурса запрещён", 403)
		return
	}
	w.Header().Set("Content-Type", typ)
	_, _ = w.Write(data)
}
func (s *Server) sendLocked(e event) {
	e.State.Bookmarks = append([]int{}, e.State.Bookmarks...)
	for c := range s.clients {
		select {
		case c.ch <- e:
		default: // Slow clients reconnect and receive the newest snapshot.
			for len(c.ch) > 0 {
				<-c.ch
			}
			c.ch <- event{"prepare", e.State}
		}
	}
}
func (s *Server) publishLocked() { s.state.Revision++; s.sendLocked(event{"prepare", s.state}) }
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	c := &client{ch: make(chan event, 8)}
	s.mu.Lock()
	s.clients[c] = true
	s.publishLocked()
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, c); s.publishLocked(); s.mu.Unlock() }()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	// A per-connection ID binds readiness to this exact stream.
	id := fmt.Sprintf("%p", c)
	fmt.Fprintf(w, "event: hello\ndata: %q\n\n", id)
	fl.Flush()
	for {
		select {
		case e := <-c.ch:
			data, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			fl.Flush()
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Client   string `json:"client"`
		Revision uint64 `json:"revision"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	}
	if !decode(w, r, &q) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for c := range s.clients {
		if fmt.Sprintf("%p", c) == q.Client {
			c.width = max(160, min(4000, q.Width))
			c.height = max(200, min(6000, q.Height))
			if q.Revision == s.state.Revision {
				c.ready = q.Revision
			}
			found = true
		}
	}
	if !found {
		http.Error(w, "stream expired", 409)
		return
	}
	width, height := 4000, 6000
	for c := range s.clients {
		if c.width > 0 {
			width = min(width, c.width)
			height = min(height, c.height)
		}
	}
	if width != s.state.Width || height != s.state.Height {
		s.state.Width = width
		s.state.Height = height
		s.publishLocked()
		writeJSON(w, false)
		return
	}
	if q.Revision != s.state.Revision {
		writeJSON(w, false)
		return
	}
	all := len(s.clients) > 0
	for c := range s.clients {
		all = all && c.ready == q.Revision
	}
	if all {
		s.sendLocked(event{"commit", s.state})
	}
	writeJSON(w, true)
}

type Action struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Number   float64 `json:"number"`
	Document string  `json:"document"`
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	var a Action
	if !decode(w, r, &a) {
		return
	}
	if a.Name == "fullscreen" {
		s.mu.Lock()
		fn := s.fullscreen
		s.mu.Unlock()
		if fn != nil {
			fn(a.Value == "exit")
		}
		writeJSON(w, true)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.Document != "" && a.Document != s.state.Document {
		http.Error(w, "document changed", 409)
		return
	}
	switch a.Name {
	case "next":
		s.state.Move(1)
	case "previous":
		s.state.Move(-1)
	case "page":
		s.state.Page = int(a.Number)
		s.state.Scroll = 0
		s.state.Anchor = ""
	case "total":
		if a.Number < 1 || a.Number > 1000000 {
			http.Error(w, "invalid count", 400)
			return
		}
		if s.state.Total == int(a.Number) {
			writeJSON(w, s.state)
			return
		}
		s.state.Total = int(a.Number)
	case "mode":
		s.state.Mode = a.Value
		s.state.Scroll = 0
	case "zoom":
		s.state.Zoom = a.Number
	case "fit":
		s.state.Fit = a.Value
	case "font":
		s.state.Font = int(a.Number)
	case "theme":
		s.state.Theme = a.Value
	case "swap":
		s.state.Swap = !s.state.Swap
	case "cover":
		s.state.Cover = !s.state.Cover
		s.state.Page = 1
	case "rtl":
		s.state.RTL = !s.state.RTL
	case "rotate":
		s.state.Rotation += 90
	case "scroll":
		s.state.Scroll = a.Number
	case "anchor":
		s.state.Anchor = a.Value
	case "locator":
		// Save a logical position without creating a render/ack feedback loop.
		if int(a.Number) == s.state.Page && len(a.Value) < 512 {
			s.state.Anchor = a.Value
			s.saveLocked()
		}
		writeJSON(w, true)
		return
	case "bookmark":
		p := s.state.Page
		found := -1
		for i, v := range s.state.Bookmarks {
			if v == p {
				found = i
			}
		}
		if found >= 0 {
			s.state.Bookmarks = append(s.state.Bookmarks[:found], s.state.Bookmarks[found+1:]...)
		} else {
			s.state.Bookmarks = append(s.state.Bookmarks, p)
			sort.Ints(s.state.Bookmarks)
		}
	default:
		http.Error(w, "unknown action", 400)
		return
	}
	s.state.Normalize()
	s.publishLocked()
	s.saveLocked()
	writeJSON(w, s.state)
}
func (s *Server) saveLocked() {
	if s.state.Document != "" {
		s.store.Sessions[s.state.Document] = s.state
	}
	s.store.Settings = s.state
	data, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil {
		return
	}
	p := filepath.Join(s.dataDir, "sessions.json")
	if err = os.WriteFile(p+".tmp", data, 0600); err == nil {
		_ = os.Rename(p+".tmp", p)
	}
}
