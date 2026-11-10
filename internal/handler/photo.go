package handler

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const maxPhotoSize = 5 << 20

var allowedPhotoExtensions = []string{".jpg", ".jpeg", ".png", ".webp"}

type Photo struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	ProductID string `json:"product_id"`
}

// Загрузка фотографии
func (s *Server) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoSize)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "требуется multipart-поле 'file' (не более 5 МБ)")
		return
	}
	defer file.Close()

	extension := strings.ToLower(filepath.Ext(header.Filename))
	if !slices.Contains(allowedPhotoExtensions, extension) {
		writeError(w, http.StatusBadRequest, "допустимые форматы: jpg, jpeg, png, webp")
		return
	}
	productID := r.FormValue("product_id")
	if productID == "" {
		writeError(w, http.StatusBadRequest, "требуется поле 'product_id'")
		return
	}

	photo := Photo{ID: uuid.NewString(), ProductID: productID}
	photo.Filename = photo.ID + extension
	path := filepath.Join(s.uploadDir, photo.Filename)

	destination, err := os.Create(path)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, copyErr := io.Copy(destination, file)
	closeErr := destination.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(path)
		writeError(w, http.StatusBadRequest, "не удалось сохранить файл")
		return
	}

	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO photos (id, filename, product_id) VALUES (?, ?, ?)`,
		photo.ID, photo.Filename, photo.ProductID)
	if err != nil {
		os.Remove(path)
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, photo)
}

// показ фотографии
func (s *Server) getPhoto(w http.ResponseWriter, r *http.Request) {
	var filename string
	err := s.db.QueryRowContext(r.Context(), `SELECT filename FROM photos WHERE id = ?`, r.PathValue("id")).Scan(&filename)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.uploadDir, filename))
}

// Удаление фотографии
func (s *Server) deletePhoto(w http.ResponseWriter, r *http.Request) {
	var filename string
	err := s.db.QueryRowContext(r.Context(), `DELETE FROM photos WHERE id = ? RETURNING filename`, r.PathValue("id")).Scan(&filename)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := os.Remove(filepath.Join(s.uploadDir, filename)); err != nil && !os.IsNotExist(err) {
		s.logger.Error("не удалось удалить файл фотографии", "файл", filename, "ошибка", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
