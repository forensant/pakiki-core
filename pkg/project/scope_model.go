package project

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScopeEntry contains the details of a single scope inclusion/exclusion
type ScopeEntry struct {
	ID             uint `json:"-"`
	GUID           string
	Name           string
	Prefix         string
	Protocol       string
	HostRegex      string
	PortRegex      string
	FileRegex      string
	IncludeInScope bool
	SortOrder      int
	ObjectType     string `gorm:"-"`
}

func (se *ScopeEntry) URLInScope(urlStr string) (bool, error) {
	if se.Prefix != "" {
		return strings.HasPrefix(urlStr, se.Prefix), nil
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return false, err
	}

	if se.Protocol != "" && u.Scheme != se.Protocol {
		return false, nil
	}

	if se.HostRegex != "" {
		// check if the host matches the regex
		re, err := regexp.Compile(se.HostRegex)
		if err != nil {
			return false, err
		}

		if !re.MatchString(u.Hostname()) {
			return false, nil
		}
	}

	if se.PortRegex != "" {
		re, err := regexp.Compile(se.PortRegex)
		if err != nil {
			return false, err
		}

		urlPort := u.Port()
		if urlPort == "" {
			if u.Scheme == "https" {
				urlPort = "443"
			} else {
				urlPort = "80"
			}
		}

		if !re.MatchString(urlPort) {
			return false, nil
		}
	}

	if se.FileRegex != "" {
		re, err := regexp.Compile(se.FileRegex)
		if err != nil {
			return false, err
		}

		if !re.MatchString(u.Path) {
			return false, nil
		}
	}

	return true, nil
}

func (se *ScopeEntry) Record() {
	if se.GUID == "" {
		se.GUID = uuid.NewString()
	}

	ioHub.databaseWriter <- se

	se.ObjectType = "Scope Entry"
	ioHub.broadcast <- se

	// hopefully this doesn't cause any race conditions
	go refreshScope()
}

func refreshScope() {
	var scopeEntries []ScopeEntry
	result := readableDatabase.Order("scope_entries.sort_order").Find(&scopeEntries)

	if result.Error != nil {
		fmt.Printf("Error retrieving scope entries from database: %s", result.Error)
		return
	}

	scope = scopeEntries
}

func (se *ScopeEntry) ShouldFilter(str string) bool {
	return false
}

func (se *ScopeEntry) WriteToDatabase(db *gorm.DB) {
	db.Save(se)
}

func (se *ScopeEntry) Validate() error {
	if se.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if se.Prefix == "" && se.HostRegex == "" && se.PortRegex == "" && se.FileRegex == "" {
		return fmt.Errorf("specify a prefix or at least one regex")
	}

	if se.HostRegex != "" {
		_, err := regexp.Compile(se.HostRegex)
		if err != nil {
			return fmt.Errorf("invalid host regex: %s", err)
		}
	}

	if se.PortRegex != "" {
		_, err := regexp.Compile(se.PortRegex)
		if err != nil {
			return fmt.Errorf("invalid port regex: %s", err)
		}
	}

	if se.FileRegex != "" {
		_, err := regexp.Compile(se.FileRegex)
		if err != nil {
			return fmt.Errorf("invalid file regex: %s", err)
		}
	}

	return nil
}
