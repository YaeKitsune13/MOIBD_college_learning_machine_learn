package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func parseRecParams(c *gin.Context) RecParams {
	return RecParams{
		Limit:    queryInt(c, "limit", 10, 1, 100),
		K:        queryInt(c, "k", 30, 1, 500),
		MinSim:   queryFloat(c, "min_sim", 0.15, 0.0, 1.0),
		MinVotes: queryInt(c, "min_votes", 2, 1, 1000),
		BayesM:   queryFloat(c, "bayes_m", 20.0, 1.0, 5000.0),
	}
}

func queryInt(c *gin.Context, key string, def, minV, maxV int) int {
	valStr := c.Query(key)
	if valStr == "" {
		return def
	}
	v, err := strconv.Atoi(valStr)
	if err != nil {
		return def
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func queryFloat(c *gin.Context, key string, def, minV, maxV float64) float64 {
	valStr := c.Query(key)
	if valStr == "" {
		return def
	}
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return def
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func registerRoutes(r *gin.Engine, db *sqlx.DB, engine *Engine) {
	r.StaticFile("/", "./web/index.html")

	api := r.Group("/api")
	{
		// Статистика датасета
		api.GET("/stats", func(c *gin.Context) {
			engine.mu.RLock()
			stats := gin.H{
				"movies_count":  len(engine.movies),
				"users_count":   len(engine.users),
				"ratings_count": engine.totalRatings,
				"genres_count":  len(engine.genreNames),
				"global_avg":    engine.globalAvgRate,
			}
			engine.mu.RUnlock()
			c.JSON(http.StatusOK, stats)
		})

		// Мгновенный список пользователей из RAM (< 1 мс вместо 3.9 сек)
		api.GET("/users", func(c *gin.Context) {
			q := c.Query("q")
			limit := queryInt(c, "limit", 100, 1, 1000)
			c.JSON(http.StatusOK, engine.GetUsers(q, limit))
		})

		// Создание нового пользователя (для проверки холодного старта без хардкода)
		api.POST("/users", func(c *gin.Context) {
			var req struct {
				Name string `json:"name"`
			}
			_ = c.ShouldBindJSON(&req)
			user, err := engine.CreateUser(req.Name)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, user)
		})

		// Быстрый поиск фильмов
		api.GET("/movies", func(c *gin.Context) {
			q := c.Query("q")
			limit := queryInt(c, "limit", 50, 1, 500)
			c.JSON(http.StatusOK, engine.SearchMovies(q, limit))
		})

		// 1. Топ-N популярных фильмов (Bayesian Average)
		api.GET("/movies/top", func(c *gin.Context) {
			p := parseRecParams(c)
			c.JSON(http.StatusOK, engine.GetTopMovies(p.Limit, p.BayesM))
		})

		// 2. Коллаборативная фильтрация (User-Based KNN)
		api.GET("/users/:id/recommendations", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
				return
			}
			p := parseRecParams(c)
			recs, isColdStart := engine.GetPersonalCollaborative(id, p)
			userRatings := engine.GetUserRatedMovies(id)

			c.JSON(http.StatusOK, gin.H{
				"user_id":       id,
				"is_cold_start": isColdStart,
				"params":        p,
				"rated_movies":  userRatings,
				"items":         recs,
			})
		})

		// 3. Прогнозирование фильмов по контенту (Content-Based)
		api.GET("/movies/:id/similar", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid movie id"})
				return
			}
			p := parseRecParams(c)
			c.JSON(http.StatusOK, engine.GetSimilarByContent(id, p.Limit, p.MinSim))
		})

		// 4. Рекомендации пользователю по его оценкам (Taste Profile)
		api.GET("/users/:id/by-ratings", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
				return
			}
			p := parseRecParams(c)
			recs, isColdStart := engine.GetPersonalByUserRatings(id, p)
			c.JSON(http.StatusOK, gin.H{
				"user_id":       id,
				"is_cold_start": isColdStart,
				"params":        p,
				"items":         recs,
			})
		})

		// Выставление оценки (инкрементальное обновление в RAM за < 1 мс)
		api.POST("/ratings", func(c *gin.Context) {
			var req Rating
			if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 0.5 || req.Rating > 5 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
				return
			}
			_, err := db.Exec(`
				INSERT INTO ratings (user_id, movie_id, rating)
				VALUES (?, ?, ?)
				ON CONFLICT(user_id, movie_id) DO UPDATE SET rating = excluded.rating
			`, req.UserID, req.MovieID, req.Rating)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			engine.UpsertRating(req.UserID, req.MovieID, req.Rating)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}
}
