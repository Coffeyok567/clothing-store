package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/joho/godotenv"

	"clothing-shop/internal/database"
	"clothing-shop/internal/handler"
)

func main() {
	_ = godotenv.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("сервер остановлен", "ошибка", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	uploadDir := getenv("UPLOAD_DIR", "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return err
	}

	// Подключение к базе и миграции
	db, err := database.Open(getenv("DATABASE_PATH", "shop.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		return err
	}
	if err := handler.EnsureDefaultAdmin(db, getenv("ADMIN_PASSWORD", "Admin123!")); err != nil {
		return err
	}

	// Запуск сервера
	server := &http.Server{
		Addr:              ":" + getenv("PORT", "8080"),
		Handler:           handler.New(db, uploadDir, logger).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownContext)
	}()

	logger.Info("сервер запущен", "адрес", server.Addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
