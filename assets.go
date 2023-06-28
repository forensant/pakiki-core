package assets

import "embed"

//go:embed www/html_frontend/dist/*
var HTMLFrontendDir embed.FS

//go:embed www/browser_home/*
var BrowserHomepageDir embed.FS

//go:embed api/swagger.json
var SwaggerJSON string

//go:embed docs/pakiki-documentation/*.md docs/pakiki-documentation/*.html docs/pakiki-documentation/_media/* docs/pakiki-documentation/getting_started/* docs/pakiki-documentation/workflows/*
var DocsDir embed.FS
