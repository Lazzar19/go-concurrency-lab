// Package web is the dashboard embedded in the server binary.
package web

import "embed"

// Files holds the page, its styles, and the TypeScript build of the client.
//
//go:embed index.html styles.css build/charts.js build/app.js
var Files embed.FS
