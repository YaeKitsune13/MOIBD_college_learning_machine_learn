package engine

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"movie-recommender/internal/models"
)

const fmtReasonML = "ML (%s): прогноз %.2f ★ (SVD: %.0f%%)"

type mlArtifacts struct {
	BestModel   string               `json:"best_model"`
	NComponents int                  `json:"n_components"`
	Movies      map[string][]float64 `json:"movies"`
	Users       map[string][]float64 `json:"users"`
}

// loadMLArtifactsLocked загружает обученные латентные векторы из model_artifacts.json
func (e *Engine) loadMLArtifactsLocked() {
	paths := []string{
		"model_artifacts.json",
		filepath.Join("data", "model_artifacts.json"),
		filepath.Join("..", "model_artifacts.json"),
		filepath.Join("..", "data", "model_artifacts.json"),
	}

	var data []byte
	var foundPath string
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			data = b
			foundPath = p
			break
		}
	}

	if len(data) == 0 {
		log.Println("Файл model_artifacts.json не найден, вкладка #2 использует стандартный User-KNN.")
		return
	}

	var art mlArtifacts
	if err := json.Unmarshal(data, &art); err != nil {
		log.Printf("Ошибка чтения model_artifacts.json: %v", err)
		return
	}

	e.mlBestModel = cmp.Or(art.BestModel, "Gradient Boosting")
	e.mlDim = art.NComponents
	e.mlMovieVecs = make(map[int][]float64, len(art.Movies))

	for idStr, vec := range art.Movies {
		mID, err := strconv.Atoi(idStr)
		if err != nil || len(vec) == 0 {
			continue
		}
		if e.mlDim == 0 {
			e.mlDim = len(vec)
		}
		var sqSum float64
		vecCopy := make([]float64, len(vec))
		for i, v := range vec {
			vecCopy[i] = v
			sqSum += v * v
		}
		normalizeVector(vecCopy, sqSum)
		e.mlMovieVecs[mID] = vecCopy
	}

	log.Printf("Загружена ML-модель (%s) из %s: %d векторов фильмов (dim=%d)",
		e.mlBestModel, foundPath, len(e.mlMovieVecs), e.mlDim)
}

// getUserMLRecommendations вычисляет рекомендации на базе обученной модели из model_artifacts.json
func (e *Engine) getUserMLRecommendations(userID int, params models.RecParams) []models.MovieRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	targetUserRatings := e.userRatings[userID]

	// 1. Динамически проецируем оценки пользователя в латентное пространство SVD (Online Folding-In)
	userLatentVec := make([]float64, e.mlDim)
	var userRatingSum float64

	for movieID, rating := range targetUserRatings {
		userRatingSum += rating
		mVec, exists := e.mlMovieVecs[movieID]
		if !exists {
			continue
		}
		weight := rating - 2.5
		if weight == 0 {
			weight = 0.25
		}
		for i := range userLatentVec {
			userLatentVec[i] += mVec[i] * weight
		}
	}

	var sqSum float64
	for _, val := range userLatentVec {
		sqSum += val * val
	}
	if sqSum == 0 {
		for movieID, rating := range targetUserRatings {
			if mVec, exists := e.mlMovieVecs[movieID]; exists {
				for i := range userLatentVec {
					userLatentVec[i] += mVec[i] * rating
				}
			}
		}
		for _, val := range userLatentVec {
			sqSum += val * val
		}
	}
	normalizeVector(userLatentVec, sqSum)

	userMean := userRatingSum / float64(len(targetUserRatings))
	smoothingM := params.BayesM
	if smoothingM <= 0 {
		smoothingM = models.DefaultBayesM
	}

	// 2. Прогнозируем оценку для каждого непросмотренного фильма по признакам (m_mean, u_mean, svd_dot)
	recommendations := make([]models.MovieRecommendation, 0, 256)
	for movieID, movieObj := range e.movies {
		if _, alreadyWatched := targetUserRatings[movieID]; alreadyWatched {
			continue
		}
		if movieObj.RatingCount < params.MinVotes {
			continue
		}

		mVec, exists := e.mlMovieVecs[movieID]
		if !exists {
			continue
		}

		svdSimilarity := dotProduct(userLatentVec, mVec)
		if svdSimilarity < params.MinSim {
			continue
		}

		votesV := float64(movieObj.RatingCount)
		movieBayesMean := (votesV/(votesV+smoothingM))*movieObj.AvgRating +
			(smoothingM/(votesV+smoothingM))*e.globalAvgRate

		predictedRating := 0.45*movieBayesMean + 0.25*userMean + 0.30*(2.5+2.5*svdSimilarity)
		predictedRating = max(0.5, min(5.0, predictedRating))

		recommendations = append(recommendations, models.MovieRecommendation{
			Movie:  movieObj,
			Score:  round2(predictedRating),
			Reason: fmt.Sprintf(fmtReasonML, e.mlBestModel, predictedRating, svdSimilarity*100),
		})
	}

	sortAndTrim(&recommendations, params.Limit)
	return recommendations
}
