package tools

type Ingredient struct {
	Name           string  `json:"name"`
	Qty            float64 `json:"qty"`
	Unit           string  `json:"unit"`
	DaysLeft       int     `json:"days_left,omitempty"`
	PerishableDays int     `json:"perishable_days,omitempty"`
	AddedDay       int     `json:"added_day,omitempty"`
}

type Pantry struct {
	Ingredients []Ingredient `json:"ingredients"`
}
