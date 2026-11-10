package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	roleAdmin   = "admin"
	roleManager = "manager"
	roleClient  = "client"
)

type Server struct {
	db        *sql.DB
	uploadDir string
	logger    *slog.Logger
}

type scanner interface {
	Scan(dest ...any) error
}

type claimsKey struct{}

func New(db *sql.DB, uploadDir string, logger *slog.Logger) *Server {
	return &Server{db: db, uploadDir: uploadDir, logger: logger}
}

// Маршруты
func (s *Server) Routes() http.Handler {
	admin := s.require(roleAdmin)
	staff := s.require(roleAdmin, roleManager)
	client := s.require(roleClient)
	anyUser := s.require(roleAdmin, roleManager, roleClient)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/login", s.login)
	mux.HandleFunc("POST /auth/register", s.register)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /auth/me", anyUser(s.me))

	mux.HandleFunc("POST /product", admin(s.createProduct))
	mux.HandleFunc("GET /product", s.listProducts)
	mux.HandleFunc("GET /product/{id}", s.getProduct)
	mux.HandleFunc("PUT /product/{id}", admin(s.updateProduct))
	mux.HandleFunc("DELETE /product/{id}", admin(s.deleteProduct))

	mux.HandleFunc("POST /photo", admin(s.uploadPhoto))
	mux.HandleFunc("GET /photo/{id}", s.getPhoto)
	mux.HandleFunc("DELETE /photo/{id}", admin(s.deletePhoto))

	mux.HandleFunc("POST /role", admin(s.createRole))
	mux.HandleFunc("GET /role", admin(s.listRoles))
	mux.HandleFunc("PUT /role/{id}", admin(s.updateRole))
	mux.HandleFunc("DELETE /role/{id}", admin(s.deleteRole))

	mux.HandleFunc("POST /user", admin(s.createUser))
	mux.HandleFunc("GET /user", admin(s.listUsers))
	mux.HandleFunc("GET /user/role/{id}", admin(s.listUsersByRole))
	mux.HandleFunc("PUT /user/{id}", admin(s.updateUser))
	mux.HandleFunc("DELETE /user/{id}", admin(s.deleteUser))

	mux.HandleFunc("POST /cart", client(s.addToCart))
	mux.HandleFunc("GET /cart/{user_id}", client(s.getCart))
	mux.HandleFunc("PUT /cart/{id}", client(s.updateCartItem))
	mux.HandleFunc("DELETE /cart/{id}", client(s.deleteCartItem))

	mux.HandleFunc("POST /order", client(s.createOrder))
	mux.HandleFunc("GET /order", anyUser(s.listOrders))
	mux.HandleFunc("GET /order/{id}", anyUser(s.getOrder))
	mux.HandleFunc("PUT /order/{id}", staff(s.updateOrder))

	mux.HandleFunc("POST /order_item", client(s.addOrderItem))
	mux.HandleFunc("GET /order_item/{order_id}", anyUser(s.listOrderItems))

	// Статические файлы сайта из каталога web (папка рядом с go.mod).
	// Все API-маршруты выше сохраняют приоритет и прежнее поведение.
	mux.Handle("/", http.FileServer(http.Dir("web")))

	return s.logRequests(mux)
}

type Claims struct {
	UserID string
	Role   string
}

// Пользователь определяется по защищённой сессионной cookie, роль берётся из базы
func (s *Server) require(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			claims, err := s.authenticate(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, err.Error())
				return
			}
			if !slices.Contains(roles, claims.Role) {
				writeError(w, http.StatusForbidden, "доступ запрещён")
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
		}
	}
}

func currentClaims(r *http.Request) Claims {
	return r.Context().Value(claimsKey{}).(Claims)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

// Лог запросов
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("запрос",
			"метод", r.Method,
			"путь", r.URL.Path,
			"статус", recorder.status,
			"длительность", time.Since(started).String(),
		)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decode(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "некорректное тело запроса")
		return false
	}
	return true
}

// Перевод ошибок базы в ответы
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	message := err.Error()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "не найдено")
	case strings.Contains(message, "UNIQUE constraint failed"):
		writeError(w, http.StatusConflict, "запись уже существует")
	case strings.Contains(message, "FOREIGN KEY constraint failed"):
		writeError(w, http.StatusBadRequest, "связанная запись не существует или всё ещё используется")
	case strings.Contains(message, "CHECK constraint failed"):
		writeError(w, http.StatusBadRequest, "значение нарушает ограничение")
	default:
		s.logger.Error("ошибка обработки запроса", "метод", r.Method, "путь", r.URL.Path, "ошибка", err)
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка сервера")
	}
}

func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func pagination(r *http.Request) (limit, offset int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return limit, (page - 1) * limit
}

func where(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}
