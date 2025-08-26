package storage

import (
	"context"
	"errors"
)

type PantryState interface {
	Load(ctx context.Context) ([]byte, error)
}

type RecipeState interface {
	Load(ctx context.Context) ([]byte, error)
}

// TestPantryState is a simple in-memory implementation for testing
type TestPantryState struct {
	data []byte
	err  error
}

func NewTestPantryState(data []byte) *TestPantryState {
	return &TestPantryState{data: data}
}

func NewTestPantryStateWithError() *TestPantryState {
	return &TestPantryState{err: errors.New("not found")}
}

func (t *TestPantryState) Load(ctx context.Context) ([]byte, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.data, nil
}

// TestRecipeState is a simple in-memory implementation for testing
type TestRecipeState struct {
	data []byte
	err  error
}

func NewTestRecipeState(data []byte) *TestRecipeState {
	return &TestRecipeState{data: data}
}

func NewTestRecipeStateWithError() *TestRecipeState {
	return &TestRecipeState{err: errors.New("not found")}
}

func (t *TestRecipeState) Load(ctx context.Context) ([]byte, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.data, nil
}
