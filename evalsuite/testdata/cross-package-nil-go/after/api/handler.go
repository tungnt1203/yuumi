// Package api phục vụ HTTP API cho user.
package api

import (
	"encoding/json"
	"net/http"

	"example.com/shop/store"
)

// Handler phục vụ các endpoint /users.
type Handler struct {
	users *store.UserStore
}

// NewHandler tạo Handler dùng store cho trước.
func NewHandler(users *store.UserStore) *Handler {
	return &Handler{users: users}
}

type userResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// GetUser xử lý GET /users/{id}.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := h.users.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, userResponse{ID: u.ID, Name: u.Name, Email: u.Email})
}

// ListUsers xử lý GET /users.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, u := range users {
		out = append(out, userResponse{ID: u.ID, Name: u.Name, Email: u.Email})
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
