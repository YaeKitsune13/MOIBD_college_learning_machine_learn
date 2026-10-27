package main

import (
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	db := initDB()
	engine := NewEngine(db)

	r := gin.Default()
	registerRoutes(r, db, engine)

	log.Println("Сервис запущен: http://localhost:8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
