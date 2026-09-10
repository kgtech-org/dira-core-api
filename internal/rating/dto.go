package rating

import "time"

// RateOrderRequest is what a client sends after a delivery.
//
// Les deux parties sont FACULTATIVES l'une comme l'autre : un client peut
// vouloir noter le restaurant sans juger le livreur, et l'inverse. Exiger les
// deux ferait inventer une note à celui qu'on ne voulait pas noter.
type RateOrderRequest struct {
	Driver *ScoreInput  `json:"driver" validate:"omitempty"`
	Stores []StoreScore `json:"stores" validate:"omitempty,max=10,dive"`
	// Dishes note les PLATS commandés. Toutes les parties sont facultatives :
	// on peut vouloir dire qu'un plat était mauvais sans juger le livreur qui
	// l'a apporté ni le restaurant qui l'a fait.
	Dishes []DishScore `json:"dishes" validate:"omitempty,max=30,dive"`
}

// ScoreInput is a score with its optional comment.
type ScoreInput struct {
	Score   int    `json:"score" validate:"required,min=1,max=5"`
	Comment string `json:"comment" validate:"omitempty,max=1000"`
}

// StoreScore rates one point of sale of the order.
type StoreScore struct {
	StoreID string `json:"store_id" validate:"required,len=24,hexadecimal"`
	Score   int    `json:"score" validate:"required,min=1,max=5"`
	Comment string `json:"comment" validate:"omitempty,max=1000"`
}

// DishScore rates one dish of the order.
type DishScore struct {
	DishID  string `json:"dish_id" validate:"required,len=24,hexadecimal"`
	Score   int    `json:"score" validate:"required,min=1,max=5"`
	Comment string `json:"comment" validate:"omitempty,max=1000"`
}

// RatingResponse is one recorded score.
type RatingResponse struct {
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Score      int       `json:"score"`
	Comment    string    `json:"comment,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SummaryResponse is what an interface displays next to a name.
//
// La MOYENNE seule ne se lit pas : 5,0 sur un avis et 4,6 sur deux cents ne
// disent pas la même chose. Le nombre l'accompagne toujours.
type SummaryResponse struct {
	Average float64 `json:"rating_avg"`
	Count   int     `json:"rating_count"`
}
