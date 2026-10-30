// Package web holds the browser UI — static HTML pages plus vendored CSS,
// fonts and images — embedded into the binary so the server has no separate
// frontend deployment step. It lives apart from internal/server so editing a
// page never touches Go server code, and the server never reaches into a
// directory of HTML by relative path.
package web

import "embed"

//go:embed static/index.html static/dailog.html static/space.html static/settings.html static/vendor
var FS embed.FS
