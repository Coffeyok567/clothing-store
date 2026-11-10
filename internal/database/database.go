package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"strings"

	_ "modernc.org/sqlite"

	"clothing-shop/migrations"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// только  одно соединение с БД
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Применение миграций
func Migrate(db *sql.DB) error {
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return err
	}
	for _, name := range names {
		content, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		// Выполняем запросы по одному: драйвер может выполнить только первый из файла
		for _, statement := range strings.Split(string(content), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				return fmt.Errorf("миграция %s: %w", name, err)
			}
		}
	}
	return nil
}
