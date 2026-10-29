package models

// Константы конфигурации
const (
	DefaultServerAddr = ":8080"
	DefaultDBDSN      = "movies.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)"
	NoGenresLabel     = "(no genres listed)"

	DefaultLimit    = 10
	DefaultK        = 30
	DefaultMinSim   = 0.15
	DefaultMinVotes = 2
	DefaultBayesM   = 20.0
	DefaultGlobalC  = 3.5

	FmtUserName      = "Пользователь #%d"
	FmtReasonBayes   = "Байес: %.2f ★ (%d оц.)"
	FmtReasonUserCF  = "User-CF: прогноз %.2f ★ (%d сос.)"
	FmtReasonItemCF  = "Item-CF: сходство %.0f%% (%d общ.)"
	FmtReasonML      = "ML (%s): прогноз %.2f ★ (SVD: %.0f%%)"
	FmtReasonContent = "Жанровое сходство: %.0f%%"
	FmtReasonHybrid  = "Вкусовой профиль: %.0f%%"
	ReasonColdStart  = "Холодный старт (Топ популярных)"
)

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

type MLArtifacts struct {
	BestModel   string               `json:"best_model"`
	NComponents int                  `json:"n_components"`
	Movies      map[string][]float64 `json:"movies"`
	Users       map[string][]float64 `json:"users"`
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

type RecParams struct {
	Limit    int     `form:"limit" json:"limit"`
	K        int     `form:"k" json:"k"`
	MinSim   float64 `form:"min_sim" json:"min_sim"`
	MinVotes int     `form:"min_votes" json:"min_votes"`
	BayesM   float64 `form:"bayes_m" json:"bayes_m"`
}

func (p *RecParams) Normalize() {
	if p.Limit == 0 {
		p.Limit = DefaultLimit
	}
	if p.K == 0 {
		p.K = DefaultK
	}
	if p.MinSim == 0 {
		p.MinSim = DefaultMinSim
	}
	if p.MinVotes == 0 {
		p.MinVotes = DefaultMinVotes
	}
	if p.BayesM == 0 {
		p.BayesM = DefaultBayesM
	}

	p.Limit = max(1, min(p.Limit, 100))
	p.K = max(1, min(p.K, 500))
	p.MinSim = max(0.01, min(p.MinSim, 1.0))
	p.MinVotes = max(1, min(p.MinVotes, 1000))
	p.BayesM = max(1.0, min(p.BayesM, 5000.0))
}
