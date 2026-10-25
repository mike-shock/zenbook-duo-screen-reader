package document

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserCorpus(t *testing.T) {
	files, err := filepath.Glob("../../books/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("local books corpus not installed")
	}
	for _, p := range files {
		if !Supported(p) {
			continue
		}
		t.Run(filepath.Base(p), func(t *testing.T) {
			b, err := Open(p)
			if err != nil {
				t.Fatal(err)
			}
			if b.ID == "" {
				t.Fatal("empty fingerprint")
			}
			if b.Format != "pdf" && len(b.HTML) < 100 {
				t.Fatal("no book text")
			}
			t.Logf("format=%s html=%d outline=%d resources=%d", b.Format, len(b.HTML), len(b.Outline), len(b.Resources))
		})
	}
}
func TestFictionMixedTextAndImage(t *testing.T) {
	b := &Book{ID: "test", Resources: map[string][]byte{}}
	err := b.fiction([]byte(`<FictionBook><description/><body><section><title><p>Глава</p></title><p>Один <emphasis>два</emphasis> три &amp; четыре</p><image href="#pic"/></section></body><binary id="pic" content-type="image/png">aGVsbG8=</binary></FictionBook>`), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.HTML, "Один <em") || !strings.Contains(b.HTML, ">два</em> три &amp; четыре") || !strings.Contains(b.HTML, "/resource/test/images/pic") {
		t.Fatal(b.HTML)
	}
	if len(b.Outline) != 1 || b.Outline[0].Title != "Глава" {
		t.Fatal(b.Outline)
	}
}
func TestEPUBSanitizer(t *testing.T) {
	b := &Book{ID: "test"}
	files := map[string][]byte{
		"META-INF/container.xml": []byte(`<container><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`),
		"OPS/content.opf":        []byte(`<package><manifest><item id="c1" href="chapter.xhtml"/></manifest><spine><itemref idref="c1"/></spine></package>`),
		"OPS/chapter.xhtml":      []byte(`<html><body onload="alert(1)"><h1 id="title">Title</h1><p>Hello<script>evil()</script><img src="https://evil.test/a"><img src="pic.png" onerror="evil()"><a href="javascript:evil()">link</a></p></body></html>`),
		"OPS/pic.png":            []byte("image"),
	}
	if err := b.epub(files); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"script", "onload", "onerror", "https:", "javascript:"} {
		if strings.Contains(b.HTML, bad) {
			t.Fatalf("unsafe %s in %s", bad, b.HTML)
		}
	}
	if !strings.Contains(b.HTML, "/resource/test/OPS/pic.png") {
		t.Fatal(b.HTML)
	}
}
func TestRejectArchiveTraversal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.fb3")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("../escape")
	_, _ = w.Write([]byte("x"))
	z.Close()
	f.Close()
	if _, err = Open(p); err == nil {
		t.Fatal("accepted traversal")
	}
}
