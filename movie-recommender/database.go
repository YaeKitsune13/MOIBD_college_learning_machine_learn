package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

var yearRegex = regexp.MustCompile(`\((\d{4})\)\s*$`)

type byteCounter struct {
	r    io.Reader
	read int64
}

func (bc *byteCounter) Read(p []byte) (int, error) {
	n, err := bc.r.Read(p)
	bc.read += int64(n)
	return n, err
}

func drawProgress(prefix string, current, total int64, rows int) {
	if total <= 0 {
		return
	}
	pct := float64(current) / float64(total) * 100
	if pct > 100 {
		pct = 100
	}
	const width = 25
	filled := int(pct / 100 * width)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Printf("\r%-22s [%s] %5.1f%% (%d строк)", prefix, bar, pct, rows)
}

func initDB() *sqlx.DB {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "movies.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)"
	}

	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		log.Fatalf("Не удалось открыть базу данных: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS movies (
		id INTEGER PRIMARY KEY,
		title TEXT NOT NULL,
		genres TEXT NOT NULL,
		year INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS ratings (
		user_id INTEGER,
		movie_id INTEGER,
		rating REAL,
		PRIMARY KEY (user_id, movie_id)
	);
	CREATE INDEX IF NOT EXISTS idx_ratings_movie ON ratings(movie_id);
	CREATE INDEX IF NOT EXISTS idx_ratings_user ON ratings(user_id);`
	db.MustExec(schema)

	// Удаляем старый захардкоженный профиль 999999, если он остался в БД с прошлых запусков
	_, _ = db.Exec(`DELETE FROM users WHERE id = 999999 AND NOT EXISTS (SELECT 1 FROM ratings WHERE user_id = 999999)`)

	var count int
	_ = db.Get(&count, "SELECT COUNT(*) FROM movies")
	if count == 0 {
		moviesPath := findDataFile("movies.csv")
		ratingsPath := findDataFile("ratings.csv")
		if moviesPath != "" && ratingsPath != "" {
			if err := importCSVFiles(db, moviesPath, ratingsPath); err != nil {
				log.Fatalf("Ошибка импорта: %v", err)
			}
		} else {
			log.Println("Файлы movies.csv и ratings.csv не найдены в ./data/, база пуста.")
		}
	}
	return db
}

func findDataFile(filename string) string {
	candidates := []string{
		filepath.Join("data", filename),
		filepath.Join("..", "data", filename),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func importCSVFiles(db *sqlx.DB, moviesPath, ratingsPath string) error {
	start := time.Now()

	db.MustExec(`DROP INDEX IF EXISTS idx_ratings_movie; DROP INDEX IF EXISTS idx_ratings_user;`)
	db.MustExec(`DELETE FROM ratings; DELETE FROM movies; DELETE FROM users;`)

	// --- 1. ИМПОРТ И ОЧИСТКА ФИЛЬМОВ (Пункт 2 ТЗ) ---
	mf, err := os.Open(moviesPath)
	if err != nil {
		return err
	}
	defer mf.Close()

	tx := db.MustBegin()
	movieStmt, err := tx.Prepare(`INSERT OR IGNORE INTO movies (id, title, genres, year) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}

	r := csv.NewReader(mf)
	_, _ = r.Read()
	movieRows := 0
	validMovies := make(map[int]struct{})

	log.Printf("Импорт и очистка фильмов из %s...", moviesPath)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 3 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		rawTitle := strings.TrimSpace(rec[1])
		genres := strings.TrimSpace(rec[2])

		// Очистка данных: пропускаем записи с некорректным ID, пустым названием или без жанров
		if err != nil || id <= 0 || rawTitle == "" || genres == "" || genres == "(no genres listed)" {
			continue
		}

		year := 0
		if matches := yearRegex.FindStringSubmatch(rawTitle); len(matches) == 2 {
			year, _ = strconv.Atoi(matches[1])
		}
		_, _ = movieStmt.Exec(id, rawTitle, genres, year)
		validMovies[id] = struct{}{}
		movieRows++
	}
	_ = movieStmt.Close()
	if err := tx.Commit(); err != nil {
		return err
	}

	// --- 2. ИМПОРТ И ОЧИСТКА ОЦЕНОК ---
	rf, err := os.Open(ratingsPath)
	if err != nil {
		return err
	}
	defer rf.Close()
	rStat, _ := rf.Stat()
	rCounter := &byteCounter{r: rf}

	userSet := make(map[int]struct{})
	rr := csv.NewReader(rCounter)
	_, _ = rr.Read()

	const batchSize = 50000
	ratingRows := 0

	tx = db.MustBegin()
	ratingStmt, _ := tx.Prepare(`INSERT OR REPLACE INTO ratings (user_id, movie_id, rating) VALUES (?, ?, ?)`)

	for {
		rec, err := rr.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 3 {
			continue
		}
		uID, err1 := strconv.Atoi(strings.TrimSpace(rec[0]))
		mID, err2 := strconv.Atoi(strings.TrimSpace(rec[1]))
		rating, err3 := strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)

		// Очистка: проверяем валидность чисел, диапазон оценки [0.5..5.0] и существование фильма
		if err1 != nil || err2 != nil || err3 != nil || uID <= 0 || rating < 0.5 || rating > 5.0 {
			continue
		}
		if _, ok := validMovies[mID]; !ok {
			continue
		}

		_, _ = ratingStmt.Exec(uID, mID, rating)
		userSet[uID] = struct{}{}
		ratingRows++

		if ratingRows%15000 == 0 {
			drawProgress("Импорт ratings.csv:", rCounter.read, rStat.Size(), ratingRows)
		}

		if ratingRows%batchSize == 0 {
			_ = ratingStmt.Close()
			_ = tx.Commit()
			tx = db.MustBegin()
			ratingStmt, _ = tx.Prepare(`INSERT OR REPLACE INTO ratings (user_id, movie_id, rating) VALUES (?, ?, ?)`)
		}
	}
	drawProgress("Импорт ratings.csv:", rStat.Size(), rStat.Size(), ratingRows)
	fmt.Println()
	_ = ratingStmt.Close()
	_ = tx.Commit()

	// --- 3. СОЗДАНИЕ ПОЛЬЗОВАТЕЛЕЙ И ИНДЕКСОВ ---
	log.Println("Генерация профилей пользователей и построение SQL-индексов...")
	tx = db.MustBegin()
	userStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO users (id, name) VALUES (?, ?)`)
	for uID := range userSet {
		_, _ = userStmt.Exec(uID, "Пользователь #"+strconv.Itoa(uID))
	}
	_ = userStmt.Close()
	_ = tx.Commit()

	db.MustExec(`
		CREATE INDEX IF NOT EXISTS idx_ratings_movie ON ratings(movie_id);
		CREATE INDEX IF NOT EXISTS idx_ratings_user ON ratings(user_id);
	`)

	log.Printf("Импорт завершен за %v! (%d фильмов, %d оценок)", time.Since(start).Round(time.Millisecond), movieRows, ratingRows)
	return nil
}
