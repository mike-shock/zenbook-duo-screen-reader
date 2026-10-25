package ui

import "embed"

//go:embed index.html app.js style.css vendor
var Files embed.FS
