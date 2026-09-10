package grocery

import (
	"errors"
	"time"
)

type ItemState string

const (
	StateNeeded    ItemState = "needed"
	StateCheck     ItemState = "check"
	StateNotNeeded ItemState = "not_needed"

	// NoGroup is the virtual group name for orphaned items.
	// It is never stored in the groups list; items carry it in their Group field.
	NoGroup = "No Group"
)

// Item is a single entry in the grocery list.
type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Group     string    `json:"group"`
	State     ItemState `json:"state"`
	Completed bool      `json:"completed"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"created_at"`
	RecipeID  string    `json:"recipe_id,omitempty"` // owning recipe, "" if none
}

// Recipe is a named, toggleable collection of items.
type Recipe struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"created_at"`
}

// Sentinel errors for recipe operations. The handler discriminates on these to
// return 400, 404 and 409 rather than collapsing everything to 404.
var (
	ErrRecipeNotFound  = errors.New("recipe not found")
	ErrDuplicateRecipe = errors.New("recipe name already exists")
	ErrRecipeOwned     = errors.New("item is owned by a recipe")
	ErrInvalidName     = errors.New("name is required")
)
