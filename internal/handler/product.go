package handler

import (
	"net/http"
	"unicode/utf8"

	"github.com/google/uuid"
)

const productColumns = "id, name, type, gender, size, color, description, brand, season, price"

type Product struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Gender      string   `json:"gender"`
	Size        string   `json:"size"`
	Color       string   `json:"color"`
	Description string   `json:"description"`
	Brand       string   `json:"brand"`
	Season      string   `json:"season"`
	Price       float64  `json:"price"`
	Photos      []string `json:"photos,omitempty"`
}

// Проверка данных товара
func (p Product) validate() string {
	switch {
	case p.Name == "" || p.Type == "" || p.Size == "" || p.Color == "" ||
		p.Description == "" || p.Brand == "" || p.Season == "":
		return "все поля товара обязательны"
	case p.Gender != "м" && p.Gender != "ж":
		return "пол gender должен быть 'м' или 'ж'"
	case utf8.RuneCountInString(p.Size) > 5:
		return "размер size не должен превышать 5 символов"
	case p.Price <= 0:
		return "цена price должна быть больше нуля"
	}
	return ""
}

func scanProduct(row scanner) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Name, &p.Type, &p.Gender, &p.Size, &p.Color,
		&p.Description, &p.Brand, &p.Season, &p.Price)
	return p, err
}

// Добавление товара
func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	var product Product
	if !decode(w, r, &product) {
		return
	}
	if message := product.validate(); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	product.ID = uuid.NewString()
	product.Photos = nil

	_, err := s.db.ExecContext(r.Context(),
		`INSERT INTO products (`+productColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		product.ID, product.Name, product.Type, product.Gender, product.Size, product.Color,
		product.Description, product.Brand, product.Season, product.Price)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, product)
}

// Список товаров: фильтры, сортировка, страницы
func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var conditions []string
	var args []any
	for _, column := range []string{"type", "gender", "size", "color", "brand", "season"} {
		if value := query.Get(column); value != "" {
			conditions = append(conditions, column+" = ?")
			args = append(args, value)
		}
	}

	statement := `SELECT ` + productColumns + ` FROM products` + where(conditions)
	switch query.Get("sort") {
	case "price_asc":
		statement += " ORDER BY price ASC"
	case "price_desc":
		statement += " ORDER BY price DESC"
	}
	limit, offset := pagination(r)
	statement += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(r.Context(), statement, args...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, products)
}

// Карточка товара с фотографиями
func (s *Server) getProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	product, err := scanProduct(s.db.QueryRowContext(r.Context(),
		`SELECT `+productColumns+` FROM products WHERE id = ?`, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `SELECT id FROM photos WHERE product_id = ?`, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var photoID string
		if err := rows.Scan(&photoID); err != nil {
			s.fail(w, r, err)
			return
		}
		product.Photos = append(product.Photos, photoID)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

// Обновление товара
func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	var product Product
	if !decode(w, r, &product) {
		return
	}
	if message := product.validate(); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	product.ID = r.PathValue("id")
	product.Photos = nil

	err := requireAffected(s.db.ExecContext(r.Context(),
		`UPDATE products SET name = ?, type = ?, gender = ?, size = ?, color = ?,
			description = ?, brand = ?, season = ?, price = ? WHERE id = ?`,
		product.Name, product.Type, product.Gender, product.Size, product.Color,
		product.Description, product.Brand, product.Season, product.Price, product.ID))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

// Удаление товара
func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	err := requireAffected(s.db.ExecContext(r.Context(), `DELETE FROM products WHERE id = ?`, r.PathValue("id")))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
