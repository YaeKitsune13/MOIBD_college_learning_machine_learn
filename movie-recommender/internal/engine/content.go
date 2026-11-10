package engine

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"movie-recommender/internal/models"
)

var yearRegex = regexp.MustCompile(`\(\d{4}\)`)

// получение самых 'топовых' фильмов
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

// Получение по жанрам и названию
func (e *Engine) GetSimilarByContent(targetMovieID int, params models.RecParams) []models.MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	targetGenreVector, exists := e.genreVectors[targetMovieID]
	if !exists {
		return nil
	}
	targetMovie := e.movies[targetMovieID]

	genreWeight := 0.6
	titleWeight := 0.4

	recommendations := make([]models.MovieRecommendation, 0, 128)
	for otherMovieID, otherGenreVector := range e.genreVectors {
		if otherMovieID == targetMovieID {
			continue
		}

		otherMovie := e.movies[otherMovieID]

		genreSimilarity := dotProduct(targetGenreVector, otherGenreVector)

		titleSimilarity := calculateTitleSimilarity(targetMovie.Title, otherMovie.Title)

		totalSimilarity := (genreSimilarity * genreWeight) + (titleSimilarity * titleWeight)

		if totalSimilarity >= params.MinSim {
			recommendations = append(recommendations, models.MovieRecommendation{
				Movie:  otherMovie,
				Score:  round2(totalSimilarity),
				Reason: fmt.Sprintf(models.FmtReasonContent, totalSimilarity*100),
			})
		}
	}

	sortAndTrim(&recommendations, params.Limit)
	return recommendations
}

// получение персональных рекомендаций
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

		if movieGenreVec, exists := e.genreVectors[movieID]; exists {
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
			if movieGenreVec, exists := e.genreVectors[movieID]; exists {
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
		// Байесовское среднее (формула IMDb): балансирует оценку фильма и число голосов.
		// Чем больше голосов (votesCountV), тем ближе итог к реальному рейтингу фильма (AvgRating).
		// Чем меньше голосов, тем сильнее оценка «стягивается» к среднему по всей базе (globalAverageC).
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

func (e *Engine) coldStartFallback(params models.RecParams) []models.MovieRecommendation {
	topMovies := e.GetTopMovies(params.Limit, params.BayesM)
	for i := range topMovies {
		topMovies[i].Reason = models.ReasonColdStart
	}
	return topMovies
}

func dotProduct(vecA, vecB []float64) float64 {
	if len(vecA) != len(vecB) {
		return 0
	}
	var sum float64
	for i := range vecA {
		sum += vecA[i] * vecB[i]
	}
	return sum
}

func calculateTitleSimilarity(title1, title2 string) float64 {
	words1 := tokenize(title1)
	words2 := tokenize(title2)

	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	intersection := 0
	for word := range words1 {
		if words2[word] {
			intersection++
		}
	}

	minLen := len(words1)
	if len(words2) < minLen {
		minLen = len(words2)
	}

	return float64(intersection) / float64(minLen)
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

func tokenize(text string) map[string]bool {
	cleanText := yearRegex.ReplaceAllString(text, "")

	words := make(map[string]bool)
	f := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsNumber(c)
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(cleanText), f) {
		if len([]rune(word)) > 2 {
			words[word] = true
		}
	}
	return words
}
