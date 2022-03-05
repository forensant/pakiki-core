package proxy

import (
	"encoding/json"
	"net/http"

	"dev.forensant.com/pipeline/razor/proximitycore/ca"
)

var interceptSettings = &InterceptSettings{
	BrowserToServer: false,
	ServerToBrowser: false,
}

// CACertificate godoc
// @Summary Gets the root CA
// @Description returns the certificate authority root certificate
// @Tags Settings
// @Produce  plain
// @Security ApiKeyAuth
// @Success 200 {string} string certificate
// @Failure 500 {string} string Error
// @Router /proxy/ca_certificate.pem [get]
func CACertificate(w http.ResponseWriter, r *http.Request) {
	certificateRecord, err := ca.CertificateForDomain("CA Root")
	if err != nil {
		http.Error(w, "Could not get certificate: "+err.Error(), http.StatusInternalServerError)
		return
	}
	pem := []byte(certificateRecord.CertificatePEM)

	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Write(pem)
}

// ProxySettings godoc
// @Summary Get Proxy Settings
// @Description get proxy settings
// @Tags Settings
// @Security ApiKeyAuth
// @Success 200 {object} proxy.ProxySettings
// @Failure 500 {string} string Error
// @Router /proxy/settings [get]
func getProxySettings(w http.ResponseWriter, r *http.Request) {
	proxySettings, err := GetSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	proxySettings.Http11ProxyListening = http11ProxyServer != nil
	proxySettings.OpenFile = ""
	proxySettings.OpenTempFile = ""
	proxySettings.OpenProcessPID = 0

	js, err := json.Marshal(proxySettings)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

// ProxySettings godoc
// @Summary Set Proxy Settings
// @Description set proxy settings
// @Tags Settings
// @Security ApiKeyAuth
// @Param default body proxy.ProxySettings true "Proxy Settings Object"
// @Success 200
// @Failure 500 {string} string Error
// @Router /proxy/settings [put]
func setProxySettings(w http.ResponseWriter, r *http.Request) {
	var proxySettings ProxySettings

	err := json.NewDecoder(r.Body).Decode(&proxySettings)
	if err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	currentSettings, err := GetSettings()
	if err != nil {
		http.Error(w, "Couldn't open current settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	proxySettings.OpenFile = currentSettings.OpenFile
	proxySettings.OpenTempFile = currentSettings.OpenTempFile
	proxySettings.OpenProcessPID = currentSettings.OpenProcessPID

	err = RestartListeners(&proxySettings)
	if err != nil {
		http.Error(w, "Error changing settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = SaveSettings(&proxySettings)
	if err != nil {
		http.Error(w, "Error changing settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	updateConnectionPool(defaultConnectionPool)
}

func HandleSettingsRequest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getProxySettings(w, r)
	case http.MethodPut:
		setProxySettings(w, r)
	default:
		http.Error(w, "Unsupported method", http.StatusInternalServerError)
	}
}
