package proxy

import (
	"net/http"
)

// GetOOBURL godoc
// @Summary Get Out of Band URL
// @Description gets a unique URL which can be used to test out of band interactions
// @Tags OutOfBand
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {string} string
// @Failure 500 {string} string Error
// @Router /proxy/out_of_band/url [get]
func GetOOBURL(w http.ResponseWriter, r *http.Request) {
	client, err := getOOBClient()

	if err != nil {
		http.Error(w, "Could not create or retrieve interactsh client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(client.URL()))
}
