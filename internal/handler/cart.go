package handler

import (
	"net/http"

	"github.com/google/uuid"
)

// корзина
const cartSelect = `SELECT c.id, c.user_id, c.product_id, p.name, p.price, c.count FROM carts c JOIN products p ON p.id = c.product_id`

type CartItem struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Price       float64 `json:"price"`
	Count       int     `json:"count"`
}

func scanCartItem(row scanner) (CartItem, error) {
	var item CartItem
	err := row.Scan(&item.ID, &item.UserID, &item.ProductID, &item.ProductName, &item.Price, &item.Count)
	return item, err
}

// Добавление в корзину
func (s *Server) addToCart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProductID string `json:"product_id"`
		Count     int    `json:"count"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.ProductID == "" || request.Count < 1 {
		writeError(w, http.StatusBadRequest, "укажите product_id и положительное количество count")
		return
	}

	userID := currentClaims(r).UserID
	_, err := s.db.ExecContext(r.Context(),
		`INSERT INTO carts (id, user_id, product_id, count) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, product_id) DO UPDATE SET count = count + excluded.count`,
		uuid.NewString(), userID, request.ProductID, request.Count)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	item, err := scanCartItem(s.db.QueryRowContext(r.Context(),
		cartSelect+` WHERE c.user_id = ? AND c.product_id = ?`, userID, request.ProductID))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// Корзина пользователя
func (s *Server) getCart(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID != currentClaims(r).UserID {
		writeError(w, http.StatusForbidden, "доступ запрещён")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), cartSelect+` WHERE c.user_id = ?`, userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()

	items := []CartItem{}
	for rows.Next() {
		item, err := scanCartItem(rows)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// Изменение количества
func (s *Server) updateCartItem(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Count int `json:"count"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.Count < 1 {
		writeError(w, http.StatusBadRequest, "количество count должно быть положительным")
		return
	}

	id := r.PathValue("id")
	err := requireAffected(s.db.ExecContext(r.Context(),
		`UPDATE carts SET count = ? WHERE id = ? AND user_id = ?`, request.Count, id, currentClaims(r).UserID))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	item, err := scanCartItem(s.db.QueryRowContext(r.Context(), cartSelect+` WHERE c.id = ?`, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Удаление из корзины
func (s *Server) deleteCartItem(w http.ResponseWriter, r *http.Request) {
	err := requireAffected(s.db.ExecContext(r.Context(),
		`DELETE FROM carts WHERE id = ? AND user_id = ?`, r.PathValue("id"), currentClaims(r).UserID))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
