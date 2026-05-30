// Package web embeds front-end static assets into the binary so the deployed
// container is a single self-contained file (no asset paths to mount).
package web

import "embed"

//go:embed static
var Static embed.FS
