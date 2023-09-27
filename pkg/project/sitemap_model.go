package project

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// SiteMapPath represents a path in the sitemap
type SiteMapPath struct {
	ID         uint   `json:"-"`
	ObjectType string `gorm:"-"`
	Path       string
	InScope    bool `gorm:"-"`
}

var siteMapPaths []SiteMapPath

func getSiteMapPath(url string) SiteMapPath {
	path := sitemapPathFromUrl(url)

	idx := sort.Search(len(siteMapPaths), func(i int) bool { return siteMapPaths[i].Path >= path })
	if idx < len(siteMapPaths) && siteMapPaths[idx].Path == path {
		// if it's already in the slice, then don't bother adding it
		return siteMapPaths[idx]
	}

	// record it in the database and UI
	item := &SiteMapPath{
		Path: path,
	}
	item.Record()

	// add it
	if idx == len(siteMapPaths) {
		siteMapPaths = append(siteMapPaths, *item)
	} else {
		siteMapPaths = append(siteMapPaths[:idx+1], siteMapPaths[idx:]...)
		siteMapPaths[idx] = *item
	}

	return *item
}

func loadSitemap(db *gorm.DB) {
	var paths []SiteMapPath
	result := db.Order("path").Find(&paths)

	siteMapPaths = append(siteMapPaths, paths...)

	if result.Error != nil {
		fmt.Printf("Error loading sitemap from the database: %s\n", result.Error.Error())
	}
}

func (siteMapPath *SiteMapPath) Record() {
	ioHub.databaseWriter <- siteMapPath

	siteMapPath.ObjectType = "Site Map Path"
	siteMapPath.InScope = urlMatchesScope(siteMapPath.Path)
	ioHub.broadcast <- siteMapPath
}

func (siteMapPath *SiteMapPath) ShouldFilter(str string) bool {
	return false
}

func sitemapPathFromUrl(url_str string) string {
	parsed_url, err := url.Parse(url_str)
	if err != nil {
		fmt.Printf("Could not parse URL: %s\n", err.Error())
		return ""
	}

	u := parsed_url.Scheme + "://" + parsed_url.Host + parsed_url.Path

	// now strip back from the last /
	lastSlashIdx := strings.LastIndex(u, "/")
	return u[0:lastSlashIdx] + "/"
}

func (siteMapPath *SiteMapPath) WriteToDatabase(db *gorm.DB) {
	db.Save(siteMapPath)
}
