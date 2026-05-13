package search

import "time"

type Doc struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	Tags          []string  `json:"tags"`
	Region        string    `json:"region"`
	Visibility    string    `json:"visibility"`
	RequiredPower string    `json:"required_power"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Filters struct {
	Types  []string
	Region string
}

type Result struct {
	ID    string
	Type  string
	Score float64
}
