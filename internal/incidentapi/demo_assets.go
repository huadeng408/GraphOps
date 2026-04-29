package incidentapi

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed demo/index.html demo/app.js demo/styles.css
var demoStaticFS embed.FS

func demoAssetsHandler() http.Handler {
	sub, err := fs.Sub(demoStaticFS, "demo")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.StripPrefix("/demo/assets/", http.FileServer(http.FS(sub)))
}
