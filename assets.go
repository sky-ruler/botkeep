package botkeep

import "embed"

// StaticFS embeds all static files (minified Tailwind CSS and HTMX script) directly into the compiled binary.
//
//go:embed static/*
var StaticFS embed.FS
