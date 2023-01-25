/* I'm not really happy with this, but Kali doesn't have the gnome-keychain
enabled and running by default. This makes secret management difficult
without asking the user to jump through hoops to install the keychain.
(It requires a reboot to have it unlocked with PAM which not really acceptable
for a desktop application)

So usability has been prioritised over security in this case :'(
*/

package proxy

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/kirsle/configdir"
)

func keyringGet(property string) (string, error) {
	filename, err := keyPath(property)

	if err != nil {
		return "", err
	}

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func keyringSet(property string, value string) error {
	path, err := keyPath(property)
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()
	_, err = file.WriteString(value)

	return err
}

func keyPath(filename string) (string, error) {
	configPath := configdir.LocalConfig("Forensant", "Proximity")
	err := configdir.MakePath(configPath) // Ensure it exists.
	if err != nil {
		return "", err
	}

	configFile := filepath.Join(configPath, strings.ToLower(strings.ReplaceAll(filename, " ", ""))+".pem")
	return configFile, nil
}
