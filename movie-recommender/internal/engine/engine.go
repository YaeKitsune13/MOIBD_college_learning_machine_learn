package engine

import (
	"cmp"
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"movie-recommender/internal/models"

	"github.com/jmoiron/sqlx"
)

type Engine struct {
	db            *sqlx.DB
	mu            sync.RWMutex
	movies        map[int]models.Movie
	movieList     []models.Movie          // Список фильмов, отсортированный по числу оценок
	users         map[int]models.User     // Кэш пользователей в памяти
	userList      []models.User           // Отсортированный список пользователей
	genreNames    []string                // Список всех уникальных жанров
	genreVectors  map[int][]float64       // movieID -> нормализованный вектор жанров фильма
	userRatings   map[int]map[int]float64 // userID -> (movieID -> оценка)
	userNorms     map[int]float64         // userID -> длина вектора оценок пользователя
	movieNorms    map[int]float64         // movieID -> длина вектора оценок фильма
	movieUsers    map[int][]int           // movieID -> список ID пользователей, оценивших фильм
	movieRateSum  map[int]float64         // movieID -> сумма всех оценок фильма
	totalRatings  int
	globalAvgRate float64
}

func NewEngine(db *sqlx.DB) *Engine {
	engine := &Engine{db: db}
	engine.ReloadIndex()
	return engine
}

// ReloadIndex загружает данные из БД в оперативную память и предрасчитывает математические нормы
func (e *Engine) ReloadIndex() {
	startTime := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()

	var rawMovies []models.Movie
	err := e.db.Select(&rawMovies, `SELECT id, title, genres, year FROM movies`)
	if err != nil {
		log.Fatalf("Ошибка чтения фильмов: %v", err)
	}

	e.movies = make(map[int]models.Movie, len(rawMovies))
	genreIndexMap := make(map[string]int)
	e.genreNames = nil

	for _, movie := range rawMovies {
		e.movies[movie.ID] = movie
		genres := strings.Split(movie.Genres, "|")
		for _, genre := range genres {
			if genre == "" || genre == models.NoGenresLabel {
				continue
			}
			_, alreadyExists := genreIndexMap[genre]
			if !alreadyExists {
				genreIndexMap[genre] = len(e.genreNames)
				e.genreNames = append(e.genreNames, genre)
			}
		}
	}

	var rawUsers []models.User
	_ = e.db.Select(&rawUsers, `SELECT id, name FROM users`)
	e.users = make(map[int]models.User, len(rawUsers))
	for _, user := range rawUsers {
		e.users[user.ID] = user
	}

	var allRatings []models.Rating
	_ = e.db.Select(&allRatings, `SELECT user_id, movie_id, rating FROM ratings`)

	e.userRatings = make(map[int]map[int]float64, len(rawUsers))
	e.userNorms = make(map[int]float64, len(rawUsers))
	e.movieNorms = make(map[int]float64, len(rawMovies))
	e.movieUsers = make(map[int][]int, len(rawMovies))
	e.movieRateSum = make(map[int]float64, len(rawMovies))

	movieRatingCounts := make(map[int]int, len(rawMovies))
	movieSquareSums := make(map[int]float64, len(rawMovies))

	var totalRatingSum float64
	for _, r := range allRatings {
		if e.userRatings[r.UserID] == nil {
			e.userRatings[r.UserID] = make(map[int]float64)
		}
		e.userRatings[r.UserID][r.MovieID] = r.Rating
		e.movieUsers[r.MovieID] = append(e.movieUsers[r.MovieID], r.UserID)

		e.movieRateSum[r.MovieID] += r.Rating
		movieSquareSums[r.MovieID] += r.Rating * r.Rating
		movieRatingCounts[r.MovieID]++
		totalRatingSum += r.Rating

		_, userExists := e.users[r.UserID]
		if !userExists {
			e.users[r.UserID] = models.User{
				ID:   r.UserID,
				Name: fmt.Sprintf(models.FmtUserName, r.UserID),
			}
		}
	}

	e.totalRatings = len(allRatings)
	if e.totalRatings > 0 {
		e.globalAvgRate = totalRatingSum / float64(e.totalRatings)
	} else {
		e.globalAvgRate = models.DefaultGlobalC
	}

	for userID, ratedMoviesMap := range e.userRatings {
		var sumOfSquares float64
		for _, ratingVal := range ratedMoviesMap {
			sumOfSquares += ratingVal * ratingVal
		}
		e.userNorms[userID] = math.Sqrt(sumOfSquares)

		userObj, exists := e.users[userID]
		if exists {
			userObj.RatingCount = len(ratedMoviesMap)
			e.users[userID] = userObj
		}
	}

	totalGenres := len(e.genreNames)
	e.genreVectors = make(map[int][]float64, len(e.movies))
	e.movieList = make([]models.Movie, 0, len(e.movies))

	for movieID, movie := range e.movies {
		count := movieRatingCounts[movieID]
		movie.RatingCount = count
		if count > 0 {
			movie.AvgRating = e.movieRateSum[movieID] / float64(count)
		}
		e.movies[movieID] = movie
		e.movieList = append(e.movieList, movie)
		e.movieNorms[movieID] = math.Sqrt(movieSquareSums[movieID])

		genreVec := make([]float64, totalGenres)
		var vectorSquareSum float64
		for _, genre := range strings.Split(movie.Genres, "|") {
			genreIdx, exists := genreIndexMap[genre]
			if exists {
				genreVec[genreIdx] = 1.0
				vectorSquareSum += 1.0
			}
		}
		normalizeVector(genreVec, vectorSquareSum)
		e.genreVectors[movieID] = genreVec
	}

	slices.SortFunc(e.movieList, func(a, b models.Movie) int {
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})
	e.rebuildUserListLocked()

	log.Printf("Индекс в RAM построен за %v: %d фильмов, %d оценок, %d пользователей",
		time.Since(startTime).Round(time.Millisecond), len(e.movies), e.totalRatings, len(e.users))
}

func (e *Engine) rebuildUserListLocked() {
	e.userList = make([]models.User, 0, len(e.users))
	for _, user := range e.users {
		e.userList = append(e.userList, user)
	}
	slices.SortFunc(e.userList, func(a, b models.User) int {
		aIsCold := a.RatingCount == 0
		bIsCold := b.RatingCount == 0
		if aIsCold != bIsCold {
			if aIsCold {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

func (e *Engine) UpsertRating(userID, movieID int, newRating float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.userRatings[userID] == nil {
		e.userRatings[userID] = make(map[int]float64)
	}

	oldRating, alreadyRated := e.userRatings[userID][movieID]
	e.userRatings[userID][movieID] = newRating

	var userSquareSum float64
	for _, r := range e.userRatings[userID] {
		userSquareSum += r * r
	}
	e.userNorms[userID] = math.Sqrt(userSquareSum)

	userObj, exists := e.users[userID]
	if !exists {
		userObj = models.User{ID: userID, Name: fmt.Sprintf(models.FmtUserName, userID)}
	}
	userObj.RatingCount = len(e.userRatings[userID])
	e.users[userID] = userObj

	totalGlobalSum := e.globalAvgRate * float64(e.totalRatings)
	movieSquareSum := e.movieNorms[movieID] * e.movieNorms[movieID]

	if alreadyRated {
		e.movieRateSum[movieID] += newRating - oldRating
		totalGlobalSum += newRating - oldRating
		movieSquareSum += (newRating * newRating) - (oldRating * oldRating)
	} else {
		e.movieUsers[movieID] = append(e.movieUsers[movieID], userID)
		e.movieRateSum[movieID] += newRating
		totalGlobalSum += newRating
		movieSquareSum += newRating * newRating
		e.totalRatings++
	}

	e.movieNorms[movieID] = math.Sqrt(max(0.0, movieSquareSum))
	if e.totalRatings > 0 {
		e.globalAvgRate = totalGlobalSum / float64(e.totalRatings)
	}

	movieObj, movieExists := e.movies[movieID]
	if movieExists {
		if !alreadyRated {
			movieObj.RatingCount++
		}
		if movieObj.RatingCount > 0 {
			movieObj.AvgRating = e.movieRateSum[movieID] / float64(movieObj.RatingCount)
		}
		e.movies[movieID] = movieObj
	}

	e.rebuildUserListLocked()
}

func (e *Engine) CreateUser(name string) (models.User, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cleanName := strings.TrimSpace(name)
	res, err := e.db.Exec(`INSERT INTO users (name) VALUES (?)`, cleanName)
	if err != nil {
		return models.User{}, err
	}

	insertedID, _ := res.LastInsertId()
	newUser := models.User{
		ID:          int(insertedID),
		Name:        cleanName,
		RatingCount: 0,
	}

	if newUser.Name == "" {
		newUser.Name = fmt.Sprintf(models.FmtUserName, newUser.ID)
		_, _ = e.db.Exec(`UPDATE users SET name = ? WHERE id = ?`, newUser.Name, newUser.ID)
	}

	e.users[newUser.ID] = newUser
	e.rebuildUserListLocked()
	return newUser, nil
}

func (e *Engine) GetUsers(searchQuery string, limit int) []models.User {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queryLower := strings.ToLower(strings.TrimSpace(searchQuery))
	result := make([]models.User, 0, min(limit, len(e.userList)))

	for _, user := range e.userList {
		matchesName := strings.Contains(strings.ToLower(user.Name), queryLower)
		matchesID := strconv.Itoa(user.ID) == queryLower

		if queryLower == "" || matchesName || matchesID {
			result = append(result, user)
			if len(result) >= limit {
				break
			}
		}
	}
	return result
}

func (e *Engine) SearchMovies(searchQuery string, limit int) []models.Movie {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queryLower := strings.ToLower(strings.TrimSpace(searchQuery))
	result := make([]models.Movie, 0, min(limit, len(e.movieList)))

	for _, cachedMovie := range e.movieList {
		matchesTitle := strings.Contains(strings.ToLower(cachedMovie.Title), queryLower)
		matchesGenre := strings.Contains(strings.ToLower(cachedMovie.Genres), queryLower)

		if queryLower == "" || matchesTitle || matchesGenre {
			actualMovie, exists := e.movies[cachedMovie.ID]
			if exists {
				result = append(result, actualMovie)
			}
			if len(result) >= limit {
				break
			}
		}
	}
	return result
}

func (e *Engine) GetTopMovies(limit int, bayesM float64) []models.MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	smoothingM := bayesM
	if smoothingM <= 0 {
		smoothingM = models.DefaultBayesM
	}
	globalAverageC := e.globalAvgRate

	recommendations := make([]models.MovieRecommendation, 0, len(e.movies)/2)

	for _, movie := range e.movies {
		if movie.RatingCount == 0 {
			continue
		}

		votesCountV := float64(movie.RatingCount)
		movieAverageR := movie.AvgRating

		weightedScore := (votesCountV/(votesCountV+smoothingM))*movieAverageR +
			(smoothingM/(votesCountV+smoothingM))*globalAverageC

		recommendations = append(recommendations, models.MovieRecommendation{
			Movie:  movie,
			Score:  round2(weightedScore),
			Reason: fmt.Sprintf(models.FmtReasonBayes, weightedScore, movie.RatingCount),
		})
	}

	sortAndTrim(&recommendations, limit)
	return recommendations
}

func (e *Engine) GetUserCollaborative(userID int, params models.RecParams) ([]models.MovieRecommendation, bool) {
	e.mu.RLock()
	targetUserRatings := e.userRatings[userID]
	targetUserNorm := e.userNorms[userID]
	e.mu.RUnlock()

	if len(targetUserRatings) == 0 || targetUserNorm == 0 {
		return e.coldStartFallback(params), true
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	dotProducts := make(map[int]float64, 512)
	for movieID, myRating := range targetUserRatings {
		usersWhoWatched := e.movieUsers[movieID]
		for _, otherUserID := range usersWhoWatched {
			if otherUserID == userID {
				continue
			}
			otherRating := e.userRatings[otherUserID][movieID]
			dotProducts[otherUserID] += myRating * otherRating
		}
	}

	type neighborUser struct {
		userID     int
		similarity float64
	}
	neighbors := make([]neighborUser, 0, len(dotProducts))

	for otherUserID, dotVal := range dotProducts {
		otherUserNorm := e.userNorms[otherUserID]
		if otherUserNorm == 0 {
			continue
		}

		cosineSim := dotVal / (targetUserNorm * otherUserNorm)
		if cosineSim >= params.MinSim {
			neighbors = append(neighbors, neighborUser{
				userID:     otherUserID,
				similarity: cosineSim,
			})
		}
	}

	slices.SortFunc(neighbors, func(a, b neighborUser) int {
		return cmp.Compare(b.similarity, a.similarity)
	})
	if len(neighbors) > params.K {
		neighbors = neighbors[:params.K]
	}

	type movieCandidate struct {
		weightedRatingSum float64
		similaritySum     float64
		neighborVotes     int
	}
	candidates := make(map[int]*movieCandidate, 256)

	for _, neighbor := range neighbors {
		neighborRatings := e.userRatings[neighbor.userID]
		for movieID, neighborRating := range neighborRatings {
			_, alreadyWatched := targetUserRatings[movieID]
			if alreadyWatched {
				continue
			}

			cand, exists := candidates[movieID]
			if !exists {
				cand = &movieCandidate{}
				candidates[movieID] = cand
			}

			cand.weightedRatingSum += neighbor.similarity * neighborRating
			cand.similaritySum += neighbor.similarity
			cand.neighborVotes++
		}
	}

	recommendations := make([]models.MovieRecommendation, 0, len(candidates))
	for movieID, cand := range candidates {
		if cand.similaritySum == 0 || cand.neighborVotes < params.MinVotes {
			continue
		}

		movieObj, exists := e.movies[movieID]
		if !exists {
			continue
		}

		predictedRating := cand.weightedRatingSum / cand.similaritySum
		recommendations = append(recommendations, models.MovieRecommendation{
			Movie:  movieObj,
			Score:  round2(predictedRating),
			Reason: fmt.Sprintf(models.FmtReasonUserCF, predictedRating, cand.neighborVotes),
		})
	}

	sortAndTrim(&recommendations, params.Limit)
	return recommendations, false
}

func (e *Engine) GetItemCollaborative(targetMovieID int, params models.RecParams) []models.MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	targetMovieNorm := e.movieNorms[targetMovieID]
	usersWhoRatedTarget := e.movieUsers[targetMovieID]

	if targetMovieNorm == 0 || len(usersWhoRatedTarget) == 0 {
		return nil
	}

	type coRatedMovie struct {
		dotProduct  float64
		commonUsers int
	}
	coRatedMap := make(map[int]*coRatedMovie, 512)

	for _, userID := range usersWhoRatedTarget {
		targetRating := e.userRatings[userID][targetMovieID]

		for otherMovieID, otherRating := range e.userRatings[userID] {
			if otherMovieID == targetMovieID {
				continue
			}

			item, exists := coRatedMap[otherMovieID]
			if !exists {
				item = &coRatedMovie{}
				coRatedMap[otherMovieID] = item
			}
			item.dotProduct += targetRating * otherRating
			item.commonUsers++
		}
	}

	recommendations := make([]models.MovieRecommendation, 0, len(coRatedMap))
	for otherMovieID, item := range coRatedMap {
		if item.commonUsers < params.MinVotes {
			continue
		}

		otherMovieNorm := e.movieNorms[otherMovieID]
		if otherMovieNorm == 0 {
			continue
		}

		cosineSim := item.dotProduct / (targetMovieNorm * otherMovieNorm)
		if cosineSim < params.MinSim {
			continue
		}

		movieObj, exists := e.movies[otherMovieID]
		if exists {
			recommendations = append(recommendations, models.MovieRecommendation{
				Movie:  movieObj,
				Score:  round2(cosineSim),
				Reason: fmt.Sprintf(models.FmtReasonItemCF, cosineSim*100, item.commonUsers),
			})
		}
	}

	sortAndTrim(&recommendations, params.Limit)
	return recommendations
}

func (e *Engine) GetSimilarByContent(targetMovieID int, params models.RecParams) []models.MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	targetGenreVector, exists := e.genreVectors[targetMovieID]
	if !exists {
		return nil
	}

	recommendations := make([]models.MovieRecommendation, 0, 128)
	for otherMovieID, otherGenreVector := range e.genreVectors {
		if otherMovieID == targetMovieID {
			continue
		}

		genreSimilarity := dotProduct(targetGenreVector, otherGenreVector)
		if genreSimilarity >= params.MinSim {
			recommendations = append(recommendations, models.MovieRecommendation{
				Movie:  e.movies[otherMovieID],
				Score:  round2(genreSimilarity),
				Reason: fmt.Sprintf(models.FmtReasonContent, genreSimilarity*100),
			})
		}
	}

	sortAndTrim(&recommendations, params.Limit)
	return recommendations
}

func (e *Engine) GetPersonalRecommendations(userID int, params models.RecParams) ([]models.MovieRecommendation, bool) {
	e.mu.RLock()
	targetUserRatings := e.userRatings[userID]
	e.mu.RUnlock()

	if len(targetUserRatings) == 0 {
		return e.coldStartFallback(params), true
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	userTasteProfile := make([]float64, len(e.genreNames))
	var profileSquareSum float64

	for movieID, rating := range targetUserRatings {
		interestWeight := rating - 2.5
		if interestWeight <= 0 {
			continue
		}

		movieGenreVec, exists := e.genreVectors[movieID]
		if exists {
			for i, val := range movieGenreVec {
				userTasteProfile[i] += val * interestWeight
			}
		}
	}

	for _, val := range userTasteProfile {
		profileSquareSum += val * val
	}

	if profileSquareSum == 0 {
		for movieID, rating := range targetUserRatings {
			movieGenreVec, exists := e.genreVectors[movieID]
			if exists {
				for i, val := range movieGenreVec {
					userTasteProfile[i] += val * rating
				}
			}
		}
		for _, val := range userTasteProfile {
			profileSquareSum += val * val
		}
	}

	normalizeVector(userTasteProfile, profileSquareSum)

	globalAverageC := e.globalAvgRate
	smoothingM := params.BayesM
	if smoothingM <= 0 {
		smoothingM = models.DefaultBayesM
	}

	recommendations := make([]models.MovieRecommendation, 0, 256)
	for movieID, movie := range e.movies {
		_, alreadyWatched := targetUserRatings[movieID]
		if alreadyWatched || movie.RatingCount < params.MinVotes {
			continue
		}

		tasteSimilarity := dotProduct(userTasteProfile, e.genreVectors[movieID])
		if tasteSimilarity < params.MinSim {
			continue
		}

		votesCountV := float64(movie.RatingCount)
		bayesRating := (votesCountV/(votesCountV+smoothingM))*movie.AvgRating +
			(smoothingM/(votesCountV+smoothingM))*globalAverageC

		hybridScore := (tasteSimilarity * 0.7) + ((bayesRating / 5.0) * 0.3)

		recommendations = append(recommendations, models.MovieRecommendation{
			Movie:  movie,
			Score:  round2(hybridScore),
			Reason: fmt.Sprintf(models.FmtReasonHybrid, tasteSimilarity*100),
		})
	}

	sortAndTrim(&recommendations, params.Limit)
	return recommendations, false
}

func (e *Engine) GetUserRatedMovies(userID int) []models.Rating {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ratedMap := e.userRatings[userID]
	result := make([]models.Rating, 0, len(ratedMap))
	for movieID, ratingVal := range ratedMap {
		result = append(result, models.Rating{
			UserID:  userID,
			MovieID: movieID,
			Rating:  ratingVal,
		})
	}
	return result
}

func (e *Engine) GetStats() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return map[string]any{
		"movies_count":  len(e.movies),
		"users_count":   len(e.users),
		"ratings_count": e.totalRatings,
		"genres_count":  len(e.genreNames),
		"global_avg":    e.globalAvgRate,
	}
}

func (e *Engine) coldStartFallback(params models.RecParams) []models.MovieRecommendation {
	topMovies := e.GetTopMovies(params.Limit, params.BayesM)
	for i := range topMovies {
		topMovies[i].Reason = models.ReasonColdStart
	}
	return topMovies
}

func dotProduct(vecA, vecB []float64) float64 {
	var sum float64
	for i := range vecA {
		sum += vecA[i] * vecB[i]
	}
	return sum
}

func normalizeVector(vec []float64, squareSum float64) {
	if squareSum <= 0 {
		return
	}
	length := math.Sqrt(squareSum)
	for i := range vec {
		vec[i] /= length
	}
}

func round2(val float64) float64 {
	return math.Round(val*100) / 100
}

func sortAndTrim(recs *[]models.MovieRecommendation, limit int) {
	list := *recs
	slices.SortFunc(list, func(a, b models.MovieRecommendation) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(b.RatingCount, a.RatingCount)
	})
	if len(list) > limit {
		*recs = list[:limit]
	}
}
