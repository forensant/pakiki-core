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
}

func GetSettings() (*ProxySettings, error) {
	configFile, err := settingsPath()
	if err != nil {
		return nil, err
	}

	settings := &ProxySettings{
		Http11ProxyAddr:       ":8888",
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
	configPath := configdir.LocalConfig("Forensant", "Proximity")
	err := configdir.MakePath(configPath) // Ensure it exists.
	if err != nil {
		return "", err
	}

	configFile := filepath.Join(configPath, "settings.json")
	return configFile, nil
}
