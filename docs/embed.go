// Package docs embeds the user-facing reference notes the assistant may search.
package docs

import "embed"

//go:embed runtime-coverage.md optional-permissions.md sources.md
var FS embed.FS
