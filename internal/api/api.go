package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"nexdrop/internal/auth"
)

type API struct {
	auth *auth.Service
}

func New(authService *auth.Service) *API {
	return &API{auth: authService}
}

func (api *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", api.login)
	mux.HandleFunc("POST /api/auth/refresh", api.refresh)
	mux.HandleFunc("POST /api/auth/logout", api.logout)
	mux.HandleFunc("GET /api/account", api.account)
	return mux
}

func (api *API) refresh(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	pair, err := api.auth.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (api *API) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil || request.Identifier == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	pair, err := api.auth.Login(r.Context(), request.Identifier, request.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (api *API) logout(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSON(r, &request); err != nil || api.auth.Logout(r.Context(), request.RefreshToken) != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) account(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	session, err := api.auth.Authenticate(r.Context(), token)
	if err != nil || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return
	}
	writeJSON(w, http.StatusOK, session.User)
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
