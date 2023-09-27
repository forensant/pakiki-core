package project

import (
	"encoding/json"
	"net/http"
	"strings"
)

// SiteMapItem represents a path in the sitemap to be returned
type SiteMapItem struct {
	Path    string
	InScope bool
}

// GetSitemap godoc
// @Summary Gets the sitemap
// @Description gets a list of all paths observed by the proxy
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param parent query string true "An optional filter on the query to restrict to specific paths"
// @Param scan_id query string true "An optional filter on the query to restrict to the paths to those seen for a particular scan"
// @Success 200 {array} project.SiteMapItem
// @Failure 500 {string} string Error
// @Router /requests/sitemap [get]
func GetSitemap(w http.ResponseWriter, r *http.Request) {
	parent := r.FormValue("parent")
	scanId := r.FormValue("scan_id")

	var parentSchemeIdx = strings.Index(parent, "://")
	if parentSchemeIdx != -1 {
		parent = parent[parentSchemeIdx+3:]
	}

	var siteMaps []SiteMapPath
	readableDatabase.Distinct("site_map_paths.path").Joins("left join requests on site_map_path_id = site_map_paths.id").Where("requests.scan_id = ?", scanId).Find(&siteMaps)

	var siteMap = make([]SiteMapItem, 0)
	if parent == "" {
		for _, s := range siteMaps {
			item := SiteMapItem{
				Path:    s.Path,
				InScope: urlMatchesScope(s.Path),
			}
			siteMap = append(siteMap, item)
		}
	} else {
		for _, s := range siteMaps {

			var prefixStart = 0
			var schemeIdx = strings.Index(s.Path, "://")
			if schemeIdx != -1 {
				prefixStart = schemeIdx + 3
			}

			if strings.HasPrefix(s.Path[prefixStart:], parent) {
				item := SiteMapItem{
					Path:    s.Path,
					InScope: urlMatchesScope(s.Path),
				}
				siteMap = append(siteMap, item)
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
