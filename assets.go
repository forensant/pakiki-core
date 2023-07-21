package assets

import "embed"

//go:embed www/html_frontend/dist/*
var HTMLFrontendDir embed.FS

//go:embed www/browser_home/*
var BrowserHomepageDir embed.FS

//go:embed www/cyberchef/build/prod/*
var CyberChefDir embed.FS

//go:embed api/swagger.json
var SwaggerJSON string

//go:embed docs/pakiki-documentation/*.md docs/pakiki-documentation/*.html docs/pakiki-documentation/_media/* docs/pakiki-documentation/getting_started/* docs/pakiki-documentation/workflows/* docs/pakiki-documentation/features/*
var DocsDir embed.FS

//go:embed third_party/highlight.min.js
var HighlightJS string

//go:embed third_party/atob.js
var AtobJS string
