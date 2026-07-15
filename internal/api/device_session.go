package api

import "net/http"

func (api *API) createDeviceSessionChallenge(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.devices.CreateSessionChallenge(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) attachDeviceSession(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		ChallengeID string `json:"challengeId"`
		Proof       []byte `json:"proof"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := api.devices.AttachSession(r.Context(), session, r.PathValue("id"), request.ChallengeID, request.Proof); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
