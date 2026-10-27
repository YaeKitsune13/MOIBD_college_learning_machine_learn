package main

type Movie struct {
	ID          int     `db:"id" json:"id"`
	Title       string  `db:"title" json:"title"`
	Genres      string  `db:"genres" json:"genres"`
	Year        int     `db:"year" json:"year"`
	AvgRating   float64 `db:"avg_rating" json:"avg_rating"`
	RatingCount int     `db:"rating_count" json:"rating_count"`
}

type MovieRecommendation struct {
	Movie
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

type User struct {
	ID          int    `db:"id" json:"id"`
	Name        string `db:"name" json:"name"`
	RatingCount int    `db:"rating_count" json:"rating_count"`
}

type Rating struct {
	UserID  int     `db:"user_id" json:"user_id"`
	MovieID int     `db:"movie_id" json:"movie_id"`
	Rating  float64 `db:"rating" json:"rating"`
}

// Параметры алгоритмов (без хардкода, передаются через query-параметры)
type RecParams struct {
	Limit    int     `json:"limit"`     // Количество рекомендаций (по умолчанию 10)
	K        int     `json:"k"`         // Число соседей KNN (по умолчанию 30)
	MinSim   float64 `json:"min_sim"`   // Минимальный порог сходства (по умолчанию 0.15)
	MinVotes int     `json:"min_votes"` // Мин. число голосов соседей / оценок (по умолчанию 2)
	BayesM   float64 `json:"bayes_m"`   // Параметр сглаживания Байеса m (по умолчанию 20)
}
