package project

import (
	"encoding/json"
	"net/http"
	"strings"
)

// GetSitemap godoc
// @Summary Gets the sitemap
// @Description gets a list of all paths observed by the proxy
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param parent query string true "An optional filter on the query to restrict to specific paths"
// @Param scanid query string true "An optional filter on the query to restrict to the paths to those seen for a particular scan"
// @Success 200 {array} string
// @Failure 500 {string} string Error
// @Router /project/sitemap [get]
func GetSitemap(w http.ResponseWriter, r *http.Request) {
	parent := r.FormValue("parent")
	scanId := r.FormValue("scanid")

	var siteMaps []SiteMapPath
	readableDatabase.Distinct("site_map_paths.path").Joins("left join requests on site_map_path_id = site_map_paths.id").Where("requests.scan_id = ?", scanId).Find(&siteMaps)

	var siteMap = make([]string, 0)
	if parent == "" {
		for _, s := range siteMaps {
			siteMap = append(siteMap, s.Path)
		}
	} else {
		for _, s := range siteMaps {

			var prefixStart = 0
			var schemeIdx = strings.Index(s.Path, "://")
			if schemeIdx != -1 {
				prefixStart = schemeIdx + 3
			}

			if strings.HasPrefix(s.Path[prefixStart:], parent) {
				siteMap = append(siteMap, s.Path)
			}
		}
	}

	response, err := json.Marshal(siteMap)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}
