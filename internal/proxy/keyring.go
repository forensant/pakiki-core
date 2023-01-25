//go:build !linux

package proxy

import "github.com/zalando/go-keyring"

const keyringService = "Proximity"

func keyringGet(property string) (string, error) {
	return keyring.Get(keyringService, property)
}

func keyringSet(property string, value string) error {
	return keyring.Set(keyringService, property, value)
}
