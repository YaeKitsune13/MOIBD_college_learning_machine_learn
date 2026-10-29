package database

import (
	"cmp"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"movie-recommender/internal/models"
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

const schemaSQL = `
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

type progressReader struct {
	r     io.Reader
	read  int64
	total int64
}

func newProgressReader(f *os.File) *progressReader {
	var size int64
	if info, err := f.Stat(); err == nil {
		size = info.Size()
	}
	return &progressReader{r: f, total: size}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	return n, err
}

func (pr *progressReader) Draw(label string, rows int) {
	if pr.total <= 0 {
		return
	}
	pct := min(100.0, float64(pr.read)/float64(pr.total)*100.0)
	const barWidth = 25
	filled := int(pct / 100.0 * barWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("\r%-22s [%s] %5.1f%% (%d строк)", label, bar, pct, rows)
}

func InitDB() *sqlx.DB {
	dsn := cmp.Or(os.Getenv("DB_DSN"), models.DefaultDBDSN)
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		log.Fatalf("Ошибка подключения к БД: %v", err)
	}

	db.MustExec(schemaSQL)

	var movieCount int
	if err := db.Get(&movieCount, `SELECT COUNT(*) FROM movies`); err == nil && movieCount == 0 {
		moviesCSV := findDataFile("movies.csv")
		ratingsCSV := findDataFile("ratings.csv")
		if moviesCSV != "" && ratingsCSV != "" {
			if err := importCSVFiles(db, moviesCSV, ratingsCSV); err != nil {
				log.Fatalf("Ошибка импорта CSV: %v", err)
			}
		} else {
			log.Println("Файлы movies.csv / ratings.csv не найдены в ./data, запущена чистая БД.")
		}
	}
	return db
}

func findDataFile(name string) string {
	for _, dir := range []string{"data", filepath.Join("..", "data"), filepath.Join("..", "..", "data")} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func importCSVFiles(db *sqlx.DB, moviesPath, ratingsPath string) error {
	start := time.Now()
	log.Printf("Начат импорт датасета из %s и %s...", moviesPath, ratingsPath)

	db.MustExec(`DROP INDEX IF EXISTS idx_ratings_movie; DROP INDEX IF EXISTS idx_ratings_user;`)
	db.MustExec(`DELETE FROM ratings; DELETE FROM movies; DELETE FROM users;`)

	validMovies, movieRows, err := importMovies(db, moviesPath)
	if err != nil {
		return err
	}

	userSet, ratingRows, err := importRatings(db, ratingsPath, validMovies)
	if err != nil {
		return err
	}

	log.Println("Генерация профилей пользователей и построение SQL-индексов...")
	tx := db.MustBegin()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO users (id, name) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	for uID := range userSet {
		_, _ = stmt.Exec(uID, fmt.Sprintf(models.FmtUserName, uID))
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		return err
	}

	db.MustExec(`
		CREATE INDEX IF NOT EXISTS idx_ratings_movie ON ratings(movie_id);
		CREATE INDEX IF NOT EXISTS idx_ratings_user ON ratings(user_id);
	`)

	log.Printf("Импорт завершен за %v: %d фильмов, %d оценок, %d пользователей",
		time.Since(start).Round(time.Millisecond), movieRows, ratingRows, len(userSet))
	return nil
}

func importMovies(db *sqlx.DB, path string) (map[int]struct{}, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	pr := newProgressReader(f)
	reader := csv.NewReader(pr)
	_, _ = reader.Read()

	tx := db.MustBegin()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO movies (id, title, genres, year) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return nil, 0, err
	}
	defer stmt.Close()

	valid := make(map[int]struct{})
	rows := 0

	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 3 {
			continue
		}

		id, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		title := strings.TrimSpace(rec[1])
		genres := strings.TrimSpace(rec[2])

		if err != nil || id <= 0 || title == "" || genres == "" || genres == models.NoGenresLabel {
			continue
		}

		year := 0
		if m := yearRegex.FindStringSubmatch(title); len(m) == 2 {
			year, _ = strconv.Atoi(m[1])
		}

		_, _ = stmt.Exec(id, title, genres, year)
		valid[id] = struct{}{}
		rows++

		if rows%2000 == 0 {
			pr.Draw("Импорт movies.csv:", rows)
		}
	}
	pr.read = pr.total
	pr.Draw("Импорт movies.csv:", rows)
	fmt.Println()

	return valid, rows, tx.Commit()
}

func importRatings(db *sqlx.DB, path string, validMovies map[int]struct{}) (map[int]struct{}, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	pr := newProgressReader(f)
	reader := csv.NewReader(pr)
	_, _ = reader.Read()

	users := make(map[int]struct{})
	rows := 0
	const batchSize = 50_000

	tx := db.MustBegin()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO ratings (user_id, movie_id, rating) VALUES (?, ?, ?)`)
	if err != nil {
		return nil, 0, err
	}

	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 3 {
			continue
		}

		uID, err1 := strconv.Atoi(strings.TrimSpace(rec[0]))
		mID, err2 := strconv.Atoi(strings.TrimSpace(rec[1]))
		rating, err3 := strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)

		if err1 != nil || err2 != nil || err3 != nil || uID <= 0 || rating < 0.5 || rating > 5.0 {
			continue
		}
		if _, ok := validMovies[mID]; !ok {
			continue
		}

		_, _ = stmt.Exec(uID, mID, rating)
		users[uID] = struct{}{}
		rows++

		if rows%15_000 == 0 {
			pr.Draw("Импорт ratings.csv:", rows)
		}
		if rows%batchSize == 0 {
			_ = stmt.Close()
			_ = tx.Commit()
			tx = db.MustBegin()
			stmt, _ = tx.Prepare(`INSERT OR REPLACE INTO ratings (user_id, movie_id, rating) VALUES (?, ?, ?)`)
		}
	}
	pr.read = pr.total
	pr.Draw("Импорт ratings.csv:", rows)
	fmt.Println()

	_ = stmt.Close()
	return users, rows, tx.Commit()
}
