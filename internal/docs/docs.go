package docs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var specYAML []byte

//go:embed index.html
var indexHTML []byte

func SpecHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Write(specYAML)
}

func UIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}
