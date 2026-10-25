package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"duoreader/internal/reader"
)

func clientID(c *client) string { return fmt.Sprintf("%p", c) }
func TestHTTPAccessAndPDFRange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.pdf")
	if err := os.WriteFile(p, []byte("%PDF-1.4\nabcdefghijk"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.OpenPath(p); err != nil {
		t.Fatal(err)
	}
	u, err := s.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := strings.Split(u, "/?")[0]
	r, err := http.Get(base + "/api/library")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	r, err = http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	cookies := r.Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie")
	}
	req, _ := http.NewRequest("GET", base+"/document/"+s.book.ID, nil)
	req.AddCookie(cookies[0])
	req.Header.Set("Range", "bytes=0-4")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != 206 || string(data) != "%PDF-" {
		t.Fatalf("%d %s", res.StatusCode, data)
	}
	req, _ = http.NewRequest("POST", base+"/api/action", strings.NewReader(`{"name":"next"}`))
	req.AddCookie(cookies[0])
	req.Header.Set("Origin", "https://evil.example")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
}
func TestTwoPhaseCommit(t *testing.T) {
	s := &Server{state: reader.Default(), clients: map[*client]bool{}}
	s.state.Revision = 7
	s.state.Width = 800
	s.state.Height = 1000
	left, right := &client{ch: make(chan event, 8)}, &client{ch: make(chan event, 8)}
	s.clients[left] = true
	s.clients[right] = true
	ready := func(c *client, revision uint64) {
		data, _ := json.Marshal(map[string]any{"client": clientID(c), "revision": revision, "width": 800, "height": 1000})
		rec := httptest.NewRecorder()
		s.ready(rec, httptest.NewRequest("POST", "/api/ready", bytes.NewReader(data)))
		if rec.Code != 200 {
			t.Fatal(rec.Body.String())
		}
	}
	ready(left, 7)
	if len(left.ch) != 0 || len(right.ch) != 0 {
		t.Fatal("committed before both screens ready")
	}
	ready(right, 6)
	if len(left.ch) != 0 {
		t.Fatal("accepted stale render")
	}
	ready(right, 7)
	if e := <-left.ch; e.Kind != "commit" || e.State.Revision != 7 {
		t.Fatal(e)
	}
	if e := <-right.ch; e.Kind != "commit" {
		t.Fatal(e)
	}
}
