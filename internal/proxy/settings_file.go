package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kirsle/configdir"
)

type ProxySettings struct {
	Http11ProxyAddr         string
	Http11UpstreamProxyAddr string
	MaxConnectionsPerHost   int
	Http11ProxyListening    bool

	OpenFile       string // cannot be set externally
	OpenTempFile   string // cannot be set externally
	OpenProcessPID int32  // cannot be set externally
}

func GetSettings() (*ProxySettings, error) {
	configFile, err := settingsPath()
	if err != nil {
		return nil, err
	}

	settings := &ProxySettings{
		Http11ProxyAddr:       "127.0.0.1:8080",
		MaxConnectionsPerHost: 2,
	}

	if _, err = os.Stat(configFile); os.IsNotExist(err) {
		// Create the new config file.
		fh, err := os.Create(configFile)
		if err != nil {
			return nil, err
		}
		defer fh.Close()

		encoder := json.NewEncoder(fh)
		encoder.Encode(settings)
	} else {
		// Load the existing file.
		fh, err := os.Open(configFile)
		if err != nil {
			return nil, err
		}
		defer fh.Close()

		decoder := json.NewDecoder(fh)
		decoder.Decode(settings)
	}

	if settings.MaxConnectionsPerHost <= 0 {
		settings.MaxConnectionsPerHost = 2
	}

	return settings, nil
}

func SaveSettings(settings *ProxySettings) error {
	configFile, err := settingsPath()
	if err != nil {
		return err
	}

	fh, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer fh.Close()

	encoder := json.NewEncoder(fh)
	return encoder.Encode(settings)
}

func settingsPath() (string, error) {
	configPath := configdir.LocalConfig("Forensant", "Pakiki")
	err := configdir.MakePath(configPath) // Ensure it exists.
	if err != nil {
		return "", err
	}

	configFile := filepath.Join(configPath, "settings.json")
	return configFile, nil
}
