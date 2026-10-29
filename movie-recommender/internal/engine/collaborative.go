package engine

import (
	"cmp"
	"fmt"
	"slices"

	"movie-recommender/internal/models"
)

// GetUserCollaborative реализует вкладку #2: ML-модель (если есть артефакты) с фоллбэком на классический User-KNN
func (e *Engine) GetUserCollaborative(userID int, params models.RecParams) ([]models.MovieRecommendation, bool) {
	e.mu.RLock()
	targetUserRatings := e.userRatings[userID]
	targetUserNorm := e.userNorms[userID]
	hasMLModel := len(e.mlMovieVecs) > 0 && e.mlDim > 0
	e.mu.RUnlock()

	if len(targetUserRatings) == 0 || targetUserNorm == 0 {
		return e.coldStartFallback(params), true
	}

	// Если загружена обученная ML-модель (model_artifacts.json), используем её во 2-й вкладке
	if hasMLModel {
		recs := e.getUserMLRecommendations(userID, params)
		if len(recs) > 0 {
			return recs, false
		}
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
			if _, alreadyWatched := targetUserRatings[movieID]; alreadyWatched {
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

// GetItemCollaborative реализует вкладку #3: Item-Based коллаборативную фильтрацию по общим зрителям
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

		if movieObj, exists := e.movies[otherMovieID]; exists {
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
