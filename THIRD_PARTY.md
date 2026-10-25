# Third-party software

- PDF.js / pdfjs-dist 6.4.299 — Mozilla and contributors, Apache License 2.0. Vendored assets and license: `internal/ui/vendor/pdfjs/`. Source: https://github.com/mozilla/pdf.js ; package: https://registry.npmjs.org/pdfjs-dist/-/pdfjs-dist-6.4.299.tgz . Assets include their upstream font, CMap and decoder notices; retain these with redistributed source/builds.
- github.com/jchv/go-webview2 — MIT, version pinned in go.mod. https://github.com/jchv/go-webview2
- github.com/jchv/go-winloader — ISC, version pinned in go.mod. https://github.com/jchv/go-winloader
- golang.org/x/net and golang.org/x/sys — Go BSD-style license. https://go.googlesource.com/net/ ; https://go.googlesource.com/sys/
- Microsoft Edge WebView2 Runtime — separately installed Microsoft runtime, not redistributed in this repository.

See `go.mod` and `go.sum` for exact Go dependency versions. User-provided books in `books/` are local test data and are not embedded in the application.
