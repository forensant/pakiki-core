package project

import (
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// SiteMapItem represents a path in the sitemap
type SiteMapPath struct {
	ID         uint   `json:"-"`
	ObjectType string `gorm:"-"`
	Path       string
}

var siteMapPaths []string

func addToSitemap(url string) {
	path := sitemapPathFromUrl(url)

	idx := sort.SearchStrings(siteMapPaths, path)
	if idx < len(siteMapPaths) && siteMapPaths[idx] == path {
		// if it's already in the slice, then don't bother adding it
		return
	}

	// add it
	if idx == len(siteMapPaths) {
		siteMapPaths = append(siteMapPaths, path)
	} else {
		siteMapPaths = append(siteMapPaths[:idx+1], siteMapPaths[idx:]...)
		siteMapPaths[idx] = path
	}

	// record it in the database and UI
	item := &SiteMapPath{
		Path: path,
	}

	item.Record()
}

func loadSitemap(db *gorm.DB) {
	var paths []SiteMapPath
	result := db.Order("path").Find(&paths)

	for _, path := range paths {
		siteMapPaths = append(siteMapPaths, path.Path)
	}

	if result.Error != nil {
		fmt.Printf("Error loading sitemap from the database: %s\n", result.Error.Error())
	}
}

func (siteMapPath *SiteMapPath) Record() {
	ioHub.databaseWriter <- siteMapPath

	siteMapPath.ObjectType = "Site Map Path"
	ioHub.broadcast <- siteMapPath
}

func (siteMapPath *SiteMapPath) ShouldFilter(str string) bool {
	return false
}

func sitemapPathFromUrl(url string) string {
	// strip the protocol
	endOfProtocol := strings.Index(url, "://")
	if endOfProtocol != -1 {
		url = url[endOfProtocol+3:]
	}

	// strip any queries
	questionMarkIdx := strings.Index(url, "?")
	if questionMarkIdx != -1 {
		url = url[0 : questionMarkIdx-1]
	}

	// now strip back from the last /
	lastSlashIdx := strings.LastIndex(url, "/")
	if lastSlashIdx == -1 {
		return ""
	}

	return url[0:lastSlashIdx]
}

func (siteMapPath *SiteMapPath) WriteToDatabase(db *gorm.DB) {
	db.Save(siteMapPath)
}
