package main

import (
	"cmp"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type Engine struct {
	db            *sqlx.DB
	mu            sync.RWMutex
	movies        map[int]Movie
	movieList     []Movie                 // Отсортированный по популярности список для быстрого поиска
	users         map[int]User            // Кэш пользователей в RAM (чтобы /api/users летал за <1 мс)
	userList      []User                  // Отсортированный список пользователей
	genreNames    []string                // Список всех жанров
	genreVectors  map[int][]float64       // movieID -> L2-нормализованный вектор жанров
	userRatings   map[int]map[int]float64 // userID -> movieID -> rating
	userNorms     map[int]float64         // userID -> sqrt(sum(r^2)) предрасчитанная норма
	movieUsers    map[int][]int           // movieID -> список userID (инвертированный индекс для мгновенного CF)
	movieRateSum  map[int]float64         // movieID -> сумма оценок (для мгновенного пересчета среднего)
	totalRatings  int
	globalAvgRate float64
}

func NewEngine(db *sqlx.DB) *Engine {
	e := &Engine{db: db}
	e.ReloadIndex()
	return e
}

func (e *Engine) ReloadIndex() {
	start := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()

	log.Println("Индексация данных в RAM...")

	// 1. Загружаем фильмы без тяжелого JOIN (агрегацию посчитаем в RAM за один проход по ratings)
	var rawMovies []Movie
	err := e.db.Select(&rawMovies, `SELECT id, title, genres, year FROM movies`)
	if err != nil {
		log.Fatalf("Ошибка загрузки фильмов: %v", err)
	}

	e.movies = make(map[int]Movie, len(rawMovies))
	genreSet := make(map[string]int)
	e.genreNames = nil

	for _, m := range rawMovies {
		e.movies[m.ID] = m
		for _, g := range strings.Split(m.Genres, "|") {
			if g == "" || g == "(no genres listed)" {
				continue
			}
			if _, ok := genreSet[g]; !ok {
				genreSet[g] = len(e.genreNames)
				e.genreNames = append(e.genreNames, g)
			}
		}
	}

	// 2. Загружаем пользователей
	var rawUsers []User
	_ = e.db.Select(&rawUsers, `SELECT id, name FROM users`)
	e.users = make(map[int]User, len(rawUsers))
	for _, u := range rawUsers {
		e.users[u.ID] = u
	}

	// 3. Загружаем оценки и строим индексы за один линейный проход в RAM
	var ratings []Rating
	_ = e.db.Select(&ratings, `SELECT user_id, movie_id, rating FROM ratings`)

	e.userRatings = make(map[int]map[int]float64, len(rawUsers))
	e.userNorms = make(map[int]float64, len(rawUsers))
	e.movieUsers = make(map[int][]int, len(rawMovies))
	e.movieRateSum = make(map[int]float64, len(rawMovies))
	movieCounts := make(map[int]int, len(rawMovies))

	var totalSum float64
	for _, r := range ratings {
		if e.userRatings[r.UserID] == nil {
			e.userRatings[r.UserID] = make(map[int]float64)
		}
		e.userRatings[r.UserID][r.MovieID] = r.Rating
		e.movieUsers[r.MovieID] = append(e.movieUsers[r.MovieID], r.UserID)
		e.movieRateSum[r.MovieID] += r.Rating
		movieCounts[r.MovieID]++
		totalSum += r.Rating

		if _, exists := e.users[r.UserID]; !exists {
			e.users[r.UserID] = User{ID: r.UserID, Name: "Пользователь #" + strconv.Itoa(r.UserID)}
		}
	}

	e.totalRatings = len(ratings)
	if e.totalRatings > 0 {
		e.globalAvgRate = totalSum / float64(e.totalRatings)
	} else {
		e.globalAvgRate = 3.5
	}

	// Предрасчет норм пользователей и количества их оценок
	for uID, rMap := range e.userRatings {
		var normSq float64
		for _, val := range rMap {
			normSq += val * val
		}
		e.userNorms[uID] = math.Sqrt(normSq)
		if u, ok := e.users[uID]; ok {
			u.RatingCount = len(rMap)
			e.users[uID] = u
		}
	}

	// Обновляем статистику фильмов и строим L2-нормализованные векторы жанров
	numGenres := len(e.genreNames)
	e.genreVectors = make(map[int][]float64, len(e.movies))
	e.movieList = make([]Movie, 0, len(e.movies))

	for id, m := range e.movies {
		cnt := movieCounts[id]
		m.RatingCount = cnt
		if cnt > 0 {
			m.AvgRating = e.movieRateSum[id] / float64(cnt)
		}
		e.movies[id] = m
		e.movieList = append(e.movieList, m)

		vec := make([]float64, numGenres)
		var normSq float64
		for _, g := range strings.Split(m.Genres, "|") {
			if idx, ok := genreSet[g]; ok {
				vec[idx] = 1.0
				normSq += 1.0
			}
		}
		if normSq > 0 {
			norm := math.Sqrt(normSq)
			for i := range vec {
				vec[i] /= norm
			}
		}
		e.genreVectors[id] = vec
	}

	// Сортируем кэш фильмов по количеству оценок для мгновенного поиска
	slices.SortFunc(e.movieList, func(a, b Movie) int {
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})

	e.rebuildUserListLocked()

	log.Printf("Индекс готов за %v: %d фильмов, %d оценок, %d пользователей",
		time.Since(start).Round(time.Millisecond), len(e.movies), e.totalRatings, len(e.users))
}

func (e *Engine) rebuildUserListLocked() {
	e.userList = make([]User, 0, len(e.users))
	for _, u := range e.users {
		e.userList = append(e.userList, u)
	}
	// Сначала пользователи с 0 оценок (для удобной проверки холодного старта), затем по ID
	slices.SortFunc(e.userList, func(a, b User) int {
		aCold := a.RatingCount == 0
		bCold := b.RatingCount == 0
		if aCold != bCold {
			if aCold {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// UpsertRating мгновенно обновляет оценку в памяти без перезагрузки всей БД
func (e *Engine) UpsertRating(userID, movieID int, newRating float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.userRatings[userID] == nil {
		e.userRatings[userID] = make(map[int]float64)
	}

	oldRating, existed := e.userRatings[userID][movieID]
	e.userRatings[userID][movieID] = newRating

	// Пересчитываем норму пользователя
	var normSq float64
	for _, r := range e.userRatings[userID] {
		normSq += r * r
	}
	e.userNorms[userID] = math.Sqrt(normSq)

	// Обновляем пользователя
	u, ok := e.users[userID]
	if !ok {
		u = User{ID: userID, Name: "Пользователь #" + strconv.Itoa(userID)}
	}
	u.RatingCount = len(e.userRatings[userID])
	e.users[userID] = u

	// Обновляем статистику фильма и глобальное среднее
	totalSum := e.globalAvgRate * float64(e.totalRatings)
	if existed {
		e.movieRateSum[movieID] += newRating - oldRating
		totalSum += newRating - oldRating
	} else {
		e.movieUsers[movieID] = append(e.movieUsers[movieID], userID)
		e.movieRateSum[movieID] += newRating
		totalSum += newRating
		e.totalRatings++
	}
	if e.totalRatings > 0 {
		e.globalAvgRate = totalSum / float64(e.totalRatings)
	}

	if m, ok := e.movies[movieID]; ok {
		if !existed {
			m.RatingCount++
		}
		if m.RatingCount > 0 {
			m.AvgRating = e.movieRateSum[movieID] / float64(m.RatingCount)
		}
		e.movies[movieID] = m
	}

	e.rebuildUserListLocked()
}

// CreateUser добавляет нового пользователя (для демонстрации холодного старта без хардкода)
func (e *Engine) CreateUser(name string) (User, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	res, err := e.db.Exec(`INSERT INTO users (name) VALUES (?)`, name)
	if err != nil {
		return User{}, err
	}
	id64, _ := res.LastInsertId()
	u := User{ID: int(id64), Name: name, RatingCount: 0}
	if u.Name == "" {
		u.Name = "Пользователь #" + strconv.Itoa(u.ID)
		_, _ = e.db.Exec(`UPDATE users SET name = ? WHERE id = ?`, u.Name, u.ID)
	}
	e.users[u.ID] = u
	e.rebuildUserListLocked()
	return u, nil
}

// GetUsers отдает список пользователей из RAM за микросекунды
func (e *Engine) GetUsers(query string, limit int) []User {
	e.mu.RLock()
	defer e.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	var res []User
	for _, u := range e.userList {
		if q == "" || strings.Contains(strings.ToLower(u.Name), q) || strconv.Itoa(u.ID) == q {
			res = append(res, u)
			if len(res) >= limit {
				break
			}
		}
	}
	return res
}

// SearchMovies ищет фильмы по предотсортированному списку в RAM
func (e *Engine) SearchMovies(query string, limit int) []Movie {
	e.mu.RLock()
	defer e.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	var res []Movie
	for _, m := range e.movieList {
		if q == "" || strings.Contains(strings.ToLower(m.Title), q) || strings.Contains(strings.ToLower(m.Genres), q) {
			if current, ok := e.movies[m.ID]; ok {
				res = append(res, current)
			}
			if len(res) >= limit {
				break
			}
		}
	}
	return res
}

// 1. Топ популярных фильмов (Взвешенный рейтинг IMDB Bayesian Average)
func (e *Engine) GetTopMovies(limit int, bayesM float64) []MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if bayesM <= 0 {
		bayesM = 20.0
	}
	C := e.globalAvgRate

	recs := make([]MovieRecommendation, 0, len(e.movies)/2)
	for _, movie := range e.movies {
		if movie.RatingCount == 0 {
			continue
		}
		v := float64(movie.RatingCount)
		R := movie.AvgRating
		weightedScore := (v/(v+bayesM))*R + (bayesM/(v+bayesM))*C
		recs = append(recs, MovieRecommendation{
			Movie:  movie,
			Score:  math.Round(weightedScore*100) / 100,
			Reason: "Байесовский рейтинг (" + strconv.Itoa(movie.RatingCount) + " оц.)",
		})
	}

	slices.SortFunc(recs, func(a, b MovieRecommendation) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})

	if len(recs) > limit {
		return recs[:limit]
	}
	return recs
}

// 2. Коллаборативная фильтрация (User-Based KNN) через инвертированный индекс
func (e *Engine) GetPersonalCollaborative(userID int, p RecParams) ([]MovieRecommendation, bool) {
	e.mu.RLock()
	targetRatings := e.userRatings[userID]
	targetNorm := e.userNorms[userID]
	e.mu.RUnlock()

	if len(targetRatings) == 0 || targetNorm == 0 {
		top := e.GetTopMovies(p.Limit, p.BayesM)
		for i := range top {
			top[i].Reason = "Холодный старт (Топ популярных)"
		}
		return top, true
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Шаг 1: Находим скалярные произведения ТОЛЬКО с теми пользователями, кто оценивал те же фильмы
	dotProducts := make(map[int]float64, 512)
	for movieID, r1 := range targetRatings {
		for _, otherID := range e.movieUsers[movieID] {
			if otherID == userID {
				continue
			}
			r2 := e.userRatings[otherID][movieID]
			dotProducts[otherID] += r1 * r2
		}
	}

	type neighbor struct {
		userID int
		sim    float64
	}
	neighbors := make([]neighbor, 0, len(dotProducts))

	for otherID, dot := range dotProducts {
		otherNorm := e.userNorms[otherID]
		if otherNorm == 0 {
			continue
		}
		sim := dot / (targetNorm * otherNorm)
		if sim >= p.MinSim {
			neighbors = append(neighbors, neighbor{userID: otherID, sim: sim})
		}
	}

	// Оставляем топ-K самых похожих соседей
	slices.SortFunc(neighbors, func(a, b neighbor) int {
		return cmp.Compare(b.sim, a.sim)
	})
	if len(neighbors) > p.K {
		neighbors = neighbors[:p.K]
	}

	// Шаг 2: Агрегируем кандидатов только из фильмов, которые смотрели K соседей (а не по всей базе!)
	type candidateAcc struct {
		weightedSum float64
		simSum      float64
		votes       int
	}
	candidates := make(map[int]*candidateAcc, 256)

	for _, n := range neighbors {
		for movieID, r := range e.userRatings[n.userID] {
			if _, watched := targetRatings[movieID]; watched {
				continue
			}
			acc, ok := candidates[movieID]
			if !ok {
				acc = &candidateAcc{}
				candidates[movieID] = acc
			}
			acc.weightedSum += n.sim * r
			acc.simSum += n.sim
			acc.votes++
		}
	}

	var recs []MovieRecommendation
	for movieID, acc := range candidates {
		if acc.simSum > 0 && acc.votes >= p.MinVotes {
			movie, exists := e.movies[movieID]
			if !exists {
				continue
			}
			predictedRating := acc.weightedSum / acc.simSum
			recs = append(recs, MovieRecommendation{
				Movie: movie,
				Score: math.Round(predictedRating*100) / 100,
				Reason: "Прогноз CF: " + strconv.FormatFloat(predictedRating, 'f', 2, 64) +
					" ★ (" + strconv.Itoa(acc.votes) + " сос.)",
			})
		}
	}

	slices.SortFunc(recs, func(a, b MovieRecommendation) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})

	if len(recs) > p.Limit {
		return recs[:p.Limit], false
	}
	return recs, false
}

// 3. Контентная фильтрация: прогнозирование N фильмов по контенту (жанрам)
func (e *Engine) GetSimilarByContent(movieID int, limit int, minSim float64) []MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	targetVec, exists := e.genreVectors[movieID]
	if !exists {
		return nil
	}

	var recs []MovieRecommendation
	for id, vec := range e.genreVectors {
		if id == movieID {
			continue
		}
		// Так как векторы уже L2-нормализованы при индексации, косинусное сходство = скалярное произведение
		var sim float64
		for i := range targetVec {
			sim += targetVec[i] * vec[i]
		}
		if sim >= minSim {
			recs = append(recs, MovieRecommendation{
				Movie:  e.movies[id],
				Score:  math.Round(sim*100) / 100,
				Reason: "Сходство жанров: " + strconv.Itoa(int(math.Round(sim*100))) + "%",
			})
		}
	}

	slices.SortFunc(recs, func(a, b MovieRecommendation) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})

	if len(recs) > limit {
		return recs[:limit]
	}
	return recs
}

// 4. Рекомендации пользователю по его собственным оценкам (Профиль предпочтений + популярность)
func (e *Engine) GetPersonalByUserRatings(userID int, p RecParams) ([]MovieRecommendation, bool) {
	e.mu.RLock()
	targetRatings := e.userRatings[userID]
	e.mu.RUnlock()

	if len(targetRatings) == 0 {
		top := e.GetTopMovies(p.Limit, p.BayesM)
		for i := range top {
			top[i].Reason = "Холодный старт (Нет оценок)"
		}
		return top, true
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Формируем взвешенный вектор предпочтений пользователя по жанрам оцененных им фильмов
	numGenres := len(e.genreNames)
	userProfile := make([]float64, numGenres)
	var profileNormSq float64

	for movieID, rating := range targetRatings {
		// Учитываем вес оценки: оценки >= 3 усиливают интерес к жанру
		weight := rating - 2.5
		if weight <= 0 {
			continue
		}
		if vec, ok := e.genreVectors[movieID]; ok {
			for i, v := range vec {
				userProfile[i] += v * weight
			}
		}
	}

	for _, val := range userProfile {
		profileNormSq += val * val
	}

	// Если пользователь ставил только единицы и двойки, берем просто средневзвешенный вектор всех его фильмов
	if profileNormSq == 0 {
		for movieID, rating := range targetRatings {
			if vec, ok := e.genreVectors[movieID]; ok {
				for i, v := range vec {
					userProfile[i] += v * rating
				}
			}
		}
		for _, val := range userProfile {
			profileNormSq += val * val
		}
	}

	profileNorm := math.Sqrt(profileNormSq)
	if profileNorm > 0 {
		for i := range userProfile {
			userProfile[i] /= profileNorm
		}
	}

	C := e.globalAvgRate
	m := p.BayesM
	if m <= 0 {
		m = 20.0
	}

	var recs []MovieRecommendation
	for movieID, movie := range e.movies {
		if _, watched := targetRatings[movieID]; watched {
			continue
		}
		if movie.RatingCount < p.MinVotes {
			continue
		}

		vec := e.genreVectors[movieID]
		var contentSim float64
		for i := range userProfile {
			contentSim += userProfile[i] * vec[i]
		}

		if contentSim < p.MinSim {
			continue
		}

		// Комбинируем совпадение с вкусовым профилем пользователя (70%) и взвешенный рейтинг фильма (30%)
		v := float64(movie.RatingCount)
		bayesRating := (v/(v+m))*movie.AvgRating + (m/(v+m))*C
		hybridScore := contentSim*0.7 + (bayesRating/5.0)*0.3

		recs = append(recs, MovieRecommendation{
			Movie:  movie,
			Score:  math.Round(hybridScore*100) / 100,
			Reason: "Совпадение со вкусом: " + strconv.Itoa(int(math.Round(contentSim*100))) + "%",
		})
	}

	slices.SortFunc(recs, func(a, b MovieRecommendation) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})

	if len(recs) > p.Limit {
		return recs[:p.Limit], false
	}
	return recs, false
}

// GetUserRatedMovies возвращает список оценок пользователя с названиями фильмов из RAM
func (e *Engine) GetUserRatedMovies(userID int) []Rating {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rMap := e.userRatings[userID]
	res := make([]Rating, 0, len(rMap))
	for mID, r := range rMap {
		res = append(res, Rating{UserID: userID, MovieID: mID, Rating: r})
	}
	return res
}
