package project

import (
	"encoding/json"
	"net/http"
)

// GetSitemap godoc
// @Summary Gets the sitemap
// @Description gets a list of all paths observed by the proxy
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {array} string
// @Failure 500 {string} string Error
// @Router /project/sitemap [get]
func GetSitemap(w http.ResponseWriter, r *http.Request) {
	response, err := json.Marshal(siteMapPaths)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}
