// Package document opens local publications and normalizes flowing books to safe HTML.
package document

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	xhtml "golang.org/x/net/html"
)

const maxEntry = 32 << 20
const maxArchive = 192 << 20

type Outline struct {
	Title  string `json:"title"`
	Target string `json:"target"`
}
type Book struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Format    string            `json:"format"`
	HTML      string            `json:"html,omitempty"`
	Outline   []Outline         `json:"outline"`
	Path      string            `json:"-"`
	Resources map[string][]byte `json:"-"`
}

func Supported(name string) bool {
	n := strings.ToLower(name)
	for _, ext := range []string{".pdf", ".epub", ".fb2", ".fb2.zip", ".fb3"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

func Open(filename string) (*Book, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > 1<<30 {
		return nil, errors.New("файл больше 1 GiB")
	}
	if !Supported(filename) {
		return nil, errors.New("поддерживаются PDF, EPUB, FB2, FB2.ZIP и FB3")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return nil, err
	}
	b := &Book{ID: hex.EncodeToString(h.Sum(nil))[:24], Name: filepath.Base(filename), Path: filename, Resources: map[string][]byte{}, Outline: []Outline{}}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".pdf" {
		var sig [5]byte
		if _, err := f.ReadAt(sig[:], 0); err != nil {
			return nil, err
		}
		if string(sig[:]) != "%PDF-" {
			return nil, errors.New("неверная сигнатура PDF")
		}
		b.Format = "pdf"
		return b, nil
	}
	b.Format = strings.TrimPrefix(ext, ".")
	if ext == ".fb2" {
		if st.Size() > maxEntry {
			return nil, errors.New("FB2 слишком большой")
		}
		if _, err = f.Seek(0, 0); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		err = b.fiction(data, false, nil)
		return b, err
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		return nil, fmt.Errorf("ZIP: %w", err)
	}
	entries := map[string][]byte{}
	var total uint64
	if len(z.File) > 5000 {
		return nil, errors.New("слишком много файлов в архиве")
	}
	for _, e := range z.File {
		if e.FileInfo().IsDir() {
			continue
		}
		name := e.Name
		if !safePath(name) {
			return nil, errors.New("небезопасный путь в архиве")
		}
		total += e.UncompressedSize64
		if e.UncompressedSize64 > maxEntry || total > maxArchive {
			return nil, errors.New("превышен лимит распаковки")
		}
		r, err := e.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(r, maxEntry+1))
		r.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(data) > maxEntry {
			return nil, errors.New("слишком большая ZIP-запись")
		}
		entries[name] = data
	}
	switch ext {
	case ".zip":
		b.Format = "fb2"
		var data []byte
		for n, d := range entries {
			if strings.HasSuffix(strings.ToLower(n), ".fb2") {
				if data != nil {
					return nil, errors.New("архив содержит несколько FB2: выберите и распакуйте нужную книгу")
				}
				data = d
			}
		}
		if data == nil {
			return nil, errors.New("в архиве нет FB2")
		}
		err = b.fiction(data, false, nil)
	case ".fb3":
		rel := struct {
			Items []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}{}
		if err = xml.Unmarshal(entries["fb3/_rels/body.xml.rels"], &rel); err != nil {
			return nil, err
		}
		refs := map[string]string{}
		for _, r := range rel.Items {
			refs[r.ID] = path.Clean(path.Join("fb3", r.Target))
		}
		b.Resources = entries
		err = b.fiction(entries["fb3/body.xml"], true, refs)
	case ".epub":
		b.Resources = entries
		err = b.epub(entries)
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func safePath(p string) bool {
	return p != "" && !strings.Contains(p, "\\") && !strings.Contains(p, ":") && !strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
func (b *Book) resource(p string) string { return "/resource/" + b.ID + "/" + escapePath(p) }
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

type fictionNode struct {
	XMLName xml.Name
	Attr    []xml.Attr    `xml:",any,attr"`
	Text    string        `xml:",chardata"`
	Inner   []fictionNode `xml:",any"`
}

// fiction uses streaming tokens to preserve interleaved text and inline markup.
func (b *Book) fiction(data []byte, fb3 bool, refs map[string]string) error {
	if len(data) == 0 {
		return errors.New("нет текста FictionBook")
	}
	if !fb3 {
		// Decode embedded images once; the token pass below ignores metadata/binary.
		var bins struct {
			Images []struct {
				ID   string `xml:"id,attr"`
				MIME string `xml:"content-type,attr"`
				Data string `xml:",chardata"`
			} `xml:"binary"`
		}
		if err := xml.Unmarshal(data, &bins); err != nil {
			return err
		}
		refs = map[string]string{}
		for _, im := range bins.Images {
			raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(im.Data), ""))
			if err != nil {
				return fmt.Errorf("изображение FB2: %w", err)
			}
			if len(raw) > maxEntry {
				return errors.New("изображение слишком большое")
			}
			key := "images/" + im.ID
			b.Resources[key] = raw
			refs[im.ID] = key
		}
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out strings.Builder
	depth := 0
	body := 0
	counter := 0
	type frame struct {
		tag    string
		title  bool
		target string
		text   strings.Builder
	}
	stack := []*frame{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > 128 {
				return errors.New("XML вложен слишком глубоко")
			}
			name := t.Name.Local
			if (name == "body" && !fb3) || (name == "fb3-body" && fb3) {
				body = depth
			}
			if body == 0 {
				continue
			}
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[a.Name.Local] = a.Value
			}
			tag := map[string]string{"body": "section", "fb3-body": "section", "section": "section", "title": "h2", "subtitle": "h3", "p": "p", "v": "p", "poem": "blockquote", "stanza": "div", "cite": "blockquote", "epigraph": "blockquote", "emphasis": "em", "em": "em", "strong": "strong", "strikethrough": "s", "sub": "sub", "sup": "sup", "code": "code", "a": "a", "div": "div", "table": "table", "tr": "tr", "td": "td", "th": "th", "empty-line": "br"}[name]
			fr := &frame{tag: tag, title: name == "title" || name == "subtitle"}
			stack = append(stack, fr)
			counter++
			id := attrs["id"]
			if id == "" {
				id = fmt.Sprintf("block-%d", counter)
			}
			fr.target = id
			if name == "image" || name == "img" {
				key := strings.TrimPrefix(attrs["href"], "#")
				if key == "" {
					key = attrs["src"]
				}
				if p, ok := refs[key]; ok {
					out.WriteString(`<img alt="" src="` + html.EscapeString(b.resource(p)) + `">`)
				}
				continue
			}
			if tag != "" {
				out.WriteString("<" + tag + ` id="` + html.EscapeString(id) + `"`)
				if tag == "a" && strings.HasPrefix(attrs["href"], "#") {
					out.WriteString(` href="` + html.EscapeString(attrs["href"]) + `"`)
				}
				out.WriteString(">")
			}
		case xml.CharData:
			if body == 0 {
				continue
			}
			out.WriteString(html.EscapeString(string(t)))
			for _, f := range stack {
				if f.title {
					f.text.Write(t)
				}
			}
		case xml.EndElement:
			if body != 0 && len(stack) > 0 {
				fr := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if fr.tag != "" && fr.tag != "br" {
					out.WriteString("</" + fr.tag + ">")
				}
				if fr.title {
					s := strings.Join(strings.Fields(fr.text.String()), " ")
					if s != "" {
						b.Outline = append(b.Outline, Outline{s, fr.target})
					}
				}
				if body == depth {
					body = 0
				}
			}
			depth--
		}
	}
	b.HTML = out.String()
	if strings.TrimSpace(b.HTML) == "" {
		return errors.New("книга не содержит body")
	}
	return nil
}

func (b *Book) epub(files map[string][]byte) error {
	var container struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(files["META-INF/container.xml"], &container); err != nil {
		return err
	}
	if len(container.Rootfiles) == 0 {
		return errors.New("EPUB без package document")
	}
	opf := container.Rootfiles[0].Path
	var pkg struct {
		Manifest []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
		Spine []struct {
			ID string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
	}
	if err := xml.Unmarshal(files[opf], &pkg); err != nil {
		return err
	}
	items := map[string]string{}
	for _, m := range pkg.Manifest {
		p, _ := url.PathUnescape(m.Href)
		items[m.ID] = path.Clean(path.Join(path.Dir(opf), p))
	}
	var out strings.Builder
	for i, item := range pkg.Spine {
		p, ok := items[item.ID]
		if !ok {
			return errors.New("неизвестная глава в EPUB spine")
		}
		data, ok := files[p]
		if !ok {
			return fmt.Errorf("нет главы %s", p)
		}
		root, err := xhtml.Parse(bytes.NewReader(data))
		if err != nil {
			return err
		}
		prefix := fmt.Sprintf("chapter-%d", i)
		out.WriteString(`<section id="` + prefix + `">`)
		var visit func(*xhtml.Node, bool)
		visit = func(n *xhtml.Node, inBody bool) {
			if n.Type == xhtml.ElementNode && n.Data == "body" {
				inBody = true
			}
			if n.Type == xhtml.TextNode && inBody {
				out.WriteString(html.EscapeString(n.Data))
				return
			}
			tag := ""
			id := ""
			var a strings.Builder
			if n.Type == xhtml.ElementNode && inBody {
				if strings.Contains("|script|style|iframe|object|embed|form|input|button|link|meta|svg|math|", "|"+n.Data+"|") {
					return
				}
				if strings.Contains("|p|div|section|article|h1|h2|h3|h4|h5|h6|span|em|strong|b|i|u|s|sub|sup|blockquote|pre|code|br|hr|ul|ol|li|table|thead|tbody|tr|td|th|img|a|figure|figcaption|", "|"+n.Data+"|") {
					tag = n.Data
				}
				for _, attr := range n.Attr {
					if attr.Key == "id" {
						id = prefix + "-" + attr.Val
						a.WriteString(` id="` + html.EscapeString(id) + `"`)
					}
					if tag == "img" && attr.Key == "src" {
						u, err := url.Parse(attr.Val)
						if err == nil && u.Scheme == "" && u.Host == "" {
							res := path.Clean(path.Join(path.Dir(p), u.Path))
							if _, ok := files[res]; ok {
								a.WriteString(` src="` + html.EscapeString(b.resource(res)) + `" alt=""`)
							}
						}
					}
					if tag == "a" && attr.Key == "href" && strings.HasPrefix(attr.Val, "#") {
						a.WriteString(` href="#` + html.EscapeString(prefix+"-"+strings.TrimPrefix(attr.Val, "#")) + `"`)
					}
				}
				if tag != "" {
					out.WriteString("<" + tag + a.String() + ">")
				}
				if tag == "h1" || tag == "h2" || tag == "h3" {
					if id == "" {
						id = prefix
					}
					title := nodeText(n)
					if title != "" {
						b.Outline = append(b.Outline, Outline{title, id})
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				visit(c, inBody)
			}
			if tag != "" && tag != "img" && tag != "br" && tag != "hr" {
				out.WriteString("</" + tag + ">")
			}
		}
		visit(root, false)
		out.WriteString("</section>")
	}
	if len(pkg.Spine) == 0 {
		return errors.New("EPUB без spine")
	}
	b.HTML = out.String()
	return nil
}
func nodeText(n *xhtml.Node) string {
	var s strings.Builder
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			s.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(s.String()), " ")
}
