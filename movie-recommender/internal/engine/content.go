package engine

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"movie-recommender/internal/models"
)

// GetTopMovies реализует вкладку #5: Взвешенный рейтинг Байеса (IMDB formula)
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

// GetSimilarByContent реализует вкладку #4: Поиск похожих фильмов по жанровым векторам
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

// GetPersonalRecommendations реализует вкладку #1: Гибридный профиль жанровых предпочтений пользователя
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
