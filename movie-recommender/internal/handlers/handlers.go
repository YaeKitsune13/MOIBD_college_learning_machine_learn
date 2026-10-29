package handlers

import (
	"net/http"
	"strconv"

	"movie-recommender/internal/engine"
	"movie-recommender/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func bindRecParams(c *gin.Context) models.RecParams {
	var p models.RecParams
	_ = c.ShouldBindQuery(&p)
	p.Normalize()
	return p
}

func RegisterRoutes(r *gin.Engine, db *sqlx.DB, eng *engine.Engine) {
	r.StaticFile("/", "./web/index.html")

	api := r.Group("/api")
	{
		api.GET("/stats", func(c *gin.Context) {
			stats := eng.GetStats()
			c.JSON(http.StatusOK, stats)
		})

		api.GET("/users", func(c *gin.Context) {
			p := bindRecParams(c)
			c.JSON(http.StatusOK, eng.GetUsers(c.Query("q"), max(p.Limit, 150)))
		})

		api.POST("/users", func(c *gin.Context) {
			var req struct {
				Name string `json:"name"`
			}
			_ = c.ShouldBindJSON(&req)
			user, err := eng.CreateUser(req.Name)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, user)
		})

		api.GET("/movies", func(c *gin.Context) {
			p := bindRecParams(c)
			c.JSON(http.StatusOK, eng.SearchMovies(c.Query("q"), max(p.Limit, 60)))
		})

		api.GET("/movies/top", func(c *gin.Context) {
			p := bindRecParams(c)
			c.JSON(http.StatusOK, eng.GetTopMovies(p.Limit, p.BayesM))
		})

		api.GET("/users/:id/collaborative", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id пользователя"})
				return
			}
			p := bindRecParams(c)
			recs, isColdStart := eng.GetUserCollaborative(id, p)
			c.JSON(http.StatusOK, gin.H{
				"user_id":       id,
				"is_cold_start": isColdStart,
				"params":        p,
				"rated_movies":  eng.GetUserRatedMovies(id),
				"items":         recs,
			})
		})

		api.GET("/movies/:id/collaborative", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id фильма"})
				return
			}
			p := bindRecParams(c)
			c.JSON(http.StatusOK, eng.GetItemCollaborative(id, p))
		})

		api.GET("/movies/:id/content", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id фильма"})
				return
			}
			p := bindRecParams(c)
			c.JSON(http.StatusOK, eng.GetSimilarByContent(id, p))
		})

		api.GET("/users/:id/personal", func(c *gin.Context) {
			id, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id пользователя"})
				return
			}
			p := bindRecParams(c)
			recs, isColdStart := eng.GetPersonalRecommendations(id, p)
			c.JSON(http.StatusOK, gin.H{
				"user_id":       id,
				"is_cold_start": isColdStart,
				"params":        p,
				"items":         recs,
			})
		})

		api.POST("/ratings", func(c *gin.Context) {
			var req models.Rating
			if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 0.5 || req.Rating > 5.0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "некорректные данные оценки"})
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
			eng.UpsertRating(req.UserID, req.MovieID, req.Rating)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}
}
