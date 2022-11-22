package assets

import "embed"

//go:embed www/dist/*
var HTMLFrontendDir embed.FS

//go:embed api/swagger.json
var SwaggerJSON string
