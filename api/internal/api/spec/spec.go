// Package spec embeds the API's OpenAPI document so it travels with the
// binary rather than drifting on disk.
package spec

import (
	_ "embed"

	goapi "github.com/anchoo2kewl/go-api"
)

//go:embed openapi.yaml
var document []byte

// Document is the served OpenAPI document. MustSpec validates at init, so a
// build that embedded a broken file fails on startup rather than serving it.
var Document = goapi.MustSpec(document, goapi.SpecYAML)
