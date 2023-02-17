package assets

import "embed"

//go:embed www/html_frontend/dist/*
var HTMLFrontendDir embed.FS

//go:embed www/browser_home/*
var BrowserHomepageDir embed.FS

//go:embed api/swagger.json
var SwaggerJSON string
