package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	sessionCookie  = "forma_session"
	sessionTTL     = 7 * 24 * time.Hour
	passwordRounds = 310000
)

var loginPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

type authCredentials struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

type authUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Login string `json:"login"`
	Role  string `json:"role"`
}

// EnsureDefaultAdmin создаёт учётные данные администратора при первом запуске.
// Пароль можно изменить через переменную окружения ADMIN_PASSWORD.
func EnsureDefaultAdmin(db *sql.DB, initialPassword string) error {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM user_credentials WHERE user_id = ?`, "00000000-0000-4000-8000-0000000000a1").Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if initialPassword == "" {
		return errors.New("ADMIN_PASSWORD не может быть пустым")
	}
	hash, err := hashPassword(initialPassword)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO user_credentials (user_id, login, password_hash) VALUES (?, 'admin', ?)`, "00000000-0000-4000-8000-0000000000a1", hash)
	return err
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request authCredentials
	if !decode(w, r, &request) {
		return
	}
	if request.Login == "" || request.Password == "" || len(request.Password) > 72 {
		writeError(w, http.StatusUnauthorized, "неверный логин или пароль")
		return
	}
	var user authUser
	var passwordHash string
	err := s.db.QueryRowContext(r.Context(), `SELECT u.id, u.name, uc.login, roles.name, uc.password_hash
		FROM user_credentials uc JOIN users u ON u.id = uc.user_id JOIN roles ON roles.id = u.role_id
		WHERE uc.login = ?`, strings.TrimSpace(request.Login)).Scan(&user.ID, &user.Name, &user.Login, &user.Role, &passwordHash)
	if err != nil || !verifyPassword(request.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "неверный логин или пароль")
		return
	}

	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		s.fail(w, r, err)
		return
	}
	token := hex.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(sessionTTL)
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hex.EncodeToString(tokenHash[:]), user.ID, expires.Format(time.RFC3339)); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(sessionTTL.Seconds())})
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if !decode(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Email = strings.TrimSpace(request.Email)
	request.Phone = strings.TrimSpace(request.Phone)
	request.Login = strings.TrimSpace(request.Login)
	if request.Name == "" || request.Email == "" || request.Phone == "" || !loginPattern.MatchString(request.Login) {
		writeError(w, http.StatusBadRequest, "заполните имя, email, телефон и логин (3–32 символа: латиница, цифры, . _ -)")
		return
	}
	if len(request.Password) < 8 || len(request.Password) > 72 {
		writeError(w, http.StatusBadRequest, "пароль должен содержать от 8 до 72 символов")
		return
	}
	if len([]rune(request.Phone)) > maxPhoneLength {
		writeError(w, http.StatusBadRequest, "телефон не должен превышать 12 символов")
		return
	}
	if _, err := s.createAccount(r.Context(), request.Name, request.Email, request.Phone, request.Login, request.Password, roleClient); err != nil {
		s.fail(w, r, err)
		return
	}
	// После регистрации пользователь автоматически получает сессию.
	s.loginWithCredentials(w, r, request.Login, request.Password)
}

func (s *Server) createAccount(ctx context.Context, name, email, phone, login, password, role string) (authUser, error) {
	id := uuid.NewString()
	hash, err := hashPassword(password)
	if err != nil {
		return authUser{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return authUser{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users (id, name, email, phone, role_id) VALUES (?, ?, ?, ?, (SELECT id FROM roles WHERE name = ?))`, id, name, email, phone, role); err != nil {
		return authUser{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_credentials (user_id, login, password_hash) VALUES (?, ?, ?)`, id, login, hash); err != nil {
		return authUser{}, err
	}
	if err = tx.Commit(); err != nil {
		return authUser{}, err
	}
	return authUser{ID: id, Name: name, Login: login, Role: role}, nil
}

func (s *Server) loginWithCredentials(w http.ResponseWriter, r *http.Request, login, password string) {
	var user authUser
	var stored string
	if err := s.db.QueryRowContext(r.Context(), `SELECT u.id, u.name, uc.login, roles.name, uc.password_hash
		FROM user_credentials uc JOIN users u ON u.id = uc.user_id JOIN roles ON roles.id = u.role_id WHERE uc.login = ?`, login).
		Scan(&user.ID, &user.Name, &user.Login, &user.Role, &stored); err != nil || !verifyPassword(password, stored) {
		writeError(w, http.StatusUnauthorized, "не удалось создать сессию")
		return
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		s.fail(w, r, err)
		return
	}
	token := hex.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(sessionTTL)
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hex.EncodeToString(tokenHash[:]), user.ID, expires.Format(time.RFC3339)); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(sessionTTL.Seconds())})
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		hash := sha256.Sum256([]byte(cookie.Value))
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = ?`, hex.EncodeToString(hash[:]))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"message": "вы вышли из аккаунта"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var user authUser
	err := s.db.QueryRowContext(r.Context(), `SELECT u.id, u.name, uc.login, roles.name FROM users u
		JOIN user_credentials uc ON uc.user_id = u.id JOIN roles ON roles.id = u.role_id WHERE u.id = ?`, claims.UserID).
		Scan(&user.ID, &user.Name, &user.Login, &user.Role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) authenticate(r *http.Request) (Claims, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return Claims{}, errors.New("требуется вход в аккаунт")
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	var claims Claims
	err = s.db.QueryRowContext(r.Context(), `SELECT u.id, roles.name FROM sessions se
		JOIN users u ON u.id = se.user_id JOIN roles ON roles.id = u.role_id
		WHERE se.token_hash = ? AND se.expires_at > ?`, hex.EncodeToString(hash[:]), time.Now().UTC().Format(time.RFC3339)).
		Scan(&claims.UserID, &claims.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Claims{}, errors.New("сессия истекла, войдите снова")
	}
	if err != nil {
		return Claims{}, errors.New("не удалось проверить сессию")
	}
	return claims, nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := pbkdf2SHA256([]byte(password), salt, passwordRounds, 32)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", passwordRounds, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derived)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	var rounds int
	if _, err := fmt.Sscanf(parts[1], "%d", &rounds); err != nil || rounds < 100000 || rounds > 1000000 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) < 16 || len(expected) != 32 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, rounds, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hLen := 32
	blocks := (keyLength + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLength]
}
