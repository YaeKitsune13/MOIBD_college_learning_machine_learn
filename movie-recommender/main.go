package main

import (
	"cmp"
	"log"
	"os"

	"movie-recommender/internal/database"
	"movie-recommender/internal/engine"
	"movie-recommender/internal/handlers"
	"movie-recommender/internal/models"

	"github.com/gin-gonic/gin"
)

func main() {
	db := database.InitDB()
	eng := engine.NewEngine(db)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	handlers.RegisterRoutes(r, db, eng)

	addr := cmp.Or(os.Getenv("SERVER_ADDR"), models.DefaultServerAddr)
	log.Printf("Сервер рекомендательной системы запущен: http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Ошибка запуска HTTP-сервера: %v", err)
	}
}
