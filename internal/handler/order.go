package handler

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
)

const (
	orderColumns = "id, user_id, created_at, status"
	// Заказ  и клиент
	orderSelect    = `SELECT o.id, o.user_id, u.name, o.created_at, o.status FROM orders o JOIN users u ON u.id = o.user_id`
	statusAccepted = "Принят"
)

var orderStatuses = []string{"Принят", "В работе", "Собран", "Готов к выдаче", "Отменён"}

type Order struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	DateTime string `json:"date_time"`
	Status   string `json:"status"`
}

type OrderItem struct {
	ID          string  `json:"id"`
	OrderID     string  `json:"order_id"`
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Price       float64 `json:"price"`
	Count       int     `json:"count"`
}

func scanOrder(row scanner) (Order, error) {
	var order Order
	err := row.Scan(&order.ID, &order.UserID, &order.UserName, &order.DateTime, &order.Status)
	return order, err
}

// Клиент видит только свои заказы
func canAccess(claims Claims, order Order) bool {
	return claims.Role != roleClient || claims.UserID == order.UserID
}

func (s *Server) findAccessibleOrder(ctx context.Context, claims Claims, id string) (Order, error) {
	order, err := scanOrder(s.db.QueryRowContext(ctx, orderSelect+` WHERE o.id = ?`, id))
	if err != nil {
		return Order{}, err
	}
	if !canAccess(claims, order) {
		return Order{}, sql.ErrNoRows
	}
	return order, nil
}

func loadCartItems(ctx context.Context, tx *sql.Tx, userID string) ([]OrderItem, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.product_id, p.price, c.count
		FROM carts c JOIN products p ON p.id = c.product_id WHERE c.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []OrderItem
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ProductID, &item.Price, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Оформление заказа из корзины
func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := currentClaims(r).UserID

	// заказ и очистка корзины
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()

	items, err := loadCartItems(ctx, tx, userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "корзина пуста")
		return
	}

	order := Order{
		ID:       uuid.NewString(),
		UserID:   userID,
		DateTime: time.Now().UTC().Format(time.RFC3339),
		Status:   statusAccepted,
	}
	if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&order.UserName); err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO orders (`+orderColumns+`) VALUES (?, ?, ?, ?)`,
		order.ID, order.UserID, order.DateTime, order.Status); err != nil {
		s.fail(w, r, err)
		return
	}
	// Цена копируется в заказ, чтобы потом не менялась
	for _, item := range items {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO order_items (id, order_id, product_id, price, count) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), order.ID, item.ProductID, item.Price, item.Count)
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM carts WHERE user_id = ?`, userID); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

// Список заказов с фильтрами
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	query := r.URL.Query()
	var conditions []string
	var args []any

	if claims.Role == roleClient {
		conditions = append(conditions, "o.user_id = ?")
		args = append(args, claims.UserID)
	}
	if status := query.Get("status"); status != "" {
		conditions = append(conditions, "o.status = ?")
		args = append(args, status)
	}
	bounds := []struct{ parameter, operator string }{{"from", ">="}, {"to", "<="}}
	for _, bound := range bounds {
		value := query.Get(bound.parameter)
		if value == "" {
			continue
		}
		moment, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "параметр '"+bound.parameter+"' должен быть в формате RFC3339")
			return
		}
		conditions = append(conditions, "o.created_at "+bound.operator+" ?")
		args = append(args, moment.UTC().Format(time.RFC3339))
	}

	limit, offset := pagination(r)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(r.Context(),
		orderSelect+where(conditions)+` ORDER BY o.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

// Один заказ
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	order, err := s.findAccessibleOrder(r.Context(), currentClaims(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

// Смена статуса (менеджер, администратор)
func (s *Server) updateOrder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &request) {
		return
	}
	if !slices.Contains(orderStatuses, request.Status) {
		writeError(w, http.StatusBadRequest, "статус должен быть одним из: Принят, В работе, Собран, Готов к выдаче, Отменён")
		return
	}

	id := r.PathValue("id")
	err := requireAffected(s.db.ExecContext(r.Context(), `UPDATE orders SET status = ? WHERE id = ?`, request.Status, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	order, err := scanOrder(s.db.QueryRowContext(r.Context(), orderSelect+` WHERE o.id = ?`, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

// Добавление позиции в заказ
func (s *Server) addOrderItem(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrderID   string `json:"order_id"`
		ProductID string `json:"product_id"`
		Count     int    `json:"count"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.OrderID == "" || request.ProductID == "" || request.Count < 1 {
		writeError(w, http.StatusBadRequest, "укажите order_id, product_id и положительное количество count")
		return
	}

	order, err := s.findAccessibleOrder(r.Context(), currentClaims(r), request.OrderID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if order.Status != statusAccepted {
		writeError(w, http.StatusConflict, "позиции можно добавлять только в заказ со статусом 'Принят'")
		return
	}

	item := OrderItem{ID: uuid.NewString(), OrderID: order.ID, ProductID: request.ProductID, Count: request.Count}
	err = s.db.QueryRowContext(r.Context(), `SELECT name, price FROM products WHERE id = ?`, item.ProductID).
		Scan(&item.ProductName, &item.Price)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO order_items (id, order_id, product_id, price, count) VALUES (?, ?, ?, ?, ?)`,
		item.ID, item.OrderID, item.ProductID, item.Price, item.Count)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// Состав заказа
func (s *Server) listOrderItems(w http.ResponseWriter, r *http.Request) {
	order, err := s.findAccessibleOrder(r.Context(), currentClaims(r), r.PathValue("order_id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT oi.id, oi.order_id, oi.product_id, p.name, oi.price, oi.count
		FROM order_items oi JOIN products p ON p.id = oi.product_id WHERE oi.order_id = ?`, order.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()

	items := []OrderItem{}
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.ProductID, &item.ProductName, &item.Price, &item.Count); err != nil {
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
