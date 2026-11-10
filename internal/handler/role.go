package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// создание
func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	var role Role
	if !decode(w, r, &role) {
		return
	}
	if strings.TrimSpace(role.Name) == "" {
		writeError(w, http.StatusBadRequest, "укажите название name")
		return
	}
	role.ID = uuid.NewString()

	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO roles (id, name) VALUES (?, ?)`, role.ID, role.Name); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

// список
func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name FROM roles ORDER BY name`)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()

	roles := []Role{}
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name); err != nil {
			s.fail(w, r, err)
			return
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

// обновление
func (s *Server) updateRole(w http.ResponseWriter, r *http.Request) {
	var role Role
	if !decode(w, r, &role) {
		return
	}
	if strings.TrimSpace(role.Name) == "" {
		writeError(w, http.StatusBadRequest, "укажите название name")
		return
	}
	role.ID = r.PathValue("id")

	err := requireAffected(s.db.ExecContext(r.Context(), `UPDATE roles SET name = ? WHERE id = ?`, role.Name, role.ID))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

// удаление
func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request) {
	err := requireAffected(s.db.ExecContext(r.Context(), `DELETE FROM roles WHERE id = ?`, r.PathValue("id")))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
