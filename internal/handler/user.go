package handler

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	userColumns    = "id, name, email, phone, (SELECT name FROM roles WHERE id = users.role_id), registration_date, (SELECT login FROM user_credentials WHERE user_id = users.id)"
	maxPhoneLength = 12
)

type User struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	Role             string `json:"role"`
	RegistrationDate string `json:"registration_date"`
	Login            string `json:"login"`
}

type userRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

// Проверка данных пользователя
func (u userRequest) validate() string {
	switch {
	case u.Name == "" || u.Email == "" || u.Phone == "" || !loginPattern.MatchString(u.Login):
		return "укажите name, email, phone и корректный login (3–32 символа)"
	case len(u.Password) < 8 || len(u.Password) > 72:
		return "password должен содержать от 8 до 72 символов"
	case utf8.RuneCountInString(u.Phone) > maxPhoneLength:
		return "телефон phone не должен превышать 12 символов"
	}
	return ""
}

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Role, &u.RegistrationDate, &u.Login)
	return u, err
}

// Создание пользователя; без роли пользователь становится клиентом
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var request userRequest
	if !decode(w, r, &request) {
		return
	}
	if message := request.validate(); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	id := uuid.NewString()
	hash, err := hashPassword(request.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO users (id, name, email, phone, role_id)
		VALUES (?, ?, ?, ?, COALESCE(NULLIF(?, ''), (SELECT id FROM roles WHERE name = 'client')))`,
		id, request.Name, request.Email, request.Phone, request.Role)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO user_credentials (user_id, login, password_hash) VALUES (?, ?, ?)`, id, request.Login, hash)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	user, err := scanUser(s.db.QueryRowContext(r.Context(), `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

// Список пользователей
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	s.queryUsers(w, r, nil, nil)
}

func (s *Server) listUsersByRole(w http.ResponseWriter, r *http.Request) {
	s.queryUsers(w, r, []string{"role_id = ?"}, []any{r.PathValue("id")})
}

func (s *Server) queryUsers(w http.ResponseWriter, r *http.Request, conditions []string, args []any) {
	limit, offset := pagination(r)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT `+userColumns+` FROM users`+where(conditions)+` ORDER BY registration_date, name LIMIT ? OFFSET ?`, args...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// Обновление пользователя
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var request userRequest
	if !decode(w, r, &request) {
		return
	}
	if request.Name == "" || request.Email == "" || request.Phone == "" {
		writeError(w, http.StatusBadRequest, "укажите name, email и phone")
		return
	}
	if utf8.RuneCountInString(request.Phone) > maxPhoneLength {
		writeError(w, http.StatusBadRequest, "телефон phone не должен превышать 12 символов")
		return
	}
	if (request.Login == "") != (request.Password == "") {
		writeError(w, http.StatusBadRequest, "для изменения логина укажите одновременно login и password")
		return
	}
	if request.Login != "" && !loginPattern.MatchString(request.Login) {
		writeError(w, http.StatusBadRequest, "некорректный login")
		return
	}
	if request.Password != "" && (len(request.Password) < 8 || len(request.Password) > 72) {
		writeError(w, http.StatusBadRequest, "password должен содержать от 8 до 72 символов")
		return
	}

	id := r.PathValue("id")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE users SET name = ?, email = ?, phone = ?,
		role_id = COALESCE(NULLIF(?, ''), role_id) WHERE id = ?`, request.Name, request.Email, request.Phone, request.Role, id)
	if err == nil {
		err = requireAffected(result, nil)
	}
	if err == nil && request.Login != "" {
		var hash string
		hash, err = hashPassword(request.Password)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO user_credentials (user_id, login, password_hash)
				VALUES (?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET login = excluded.login, password_hash = excluded.password_hash`, id, request.Login, hash)
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	user, err := scanUser(s.db.QueryRowContext(r.Context(), `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// Удаление пользователя
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	err := requireAffected(s.db.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, r.PathValue("id")))
	if err != nil {
		// Заказы нельзя терять, поэтому пользователя с заказами удалить не получится
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			writeError(w, http.StatusConflict, "нельзя удалить пользователя: у него есть заказы")
			return
		}
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
