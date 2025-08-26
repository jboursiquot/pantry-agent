package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilePantryState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantry_state_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		filename    string
		data        []byte
		expectError bool
	}{
		{
			name:        "basic pantry load",
			filename:    "pantry.json",
			data:        []byte(`{"ingredients": [{"name": "egg", "qty": 12, "unit": "count"}]}`),
			expectError: false,
		},
		{
			name:        "empty pantry file",
			filename:    "empty.json",
			data:        []byte(`{"ingredients": []}`),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.filename)

			// Create the test file
			err := os.WriteFile(filePath, tt.data, 0644)
			require.NoError(t, err)

			pantryState := NewFilePantryState(filePath)
			ctx := context.Background()

			// Load data
			loadedData, err := pantryState.Load(ctx)
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.data, loadedData)
		})
	}

	t.Run("load nonexistent pantry", func(t *testing.T) {
		nonexistentPath := filepath.Join(tmpDir, "nonexistent.json")
		pantryState := NewFilePantryState(nonexistentPath)
		_, err := pantryState.Load(context.Background())
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestFileRecipeState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recipes_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		filename    string
		data        []byte
		expectError bool
	}{
		{
			name:        "valid recipes file",
			filename:    "recipes.json",
			data:        []byte(`{"recipes": [{"id": "omelet", "name": "Basic Omelet"}]}`),
			expectError: false,
		},
		{
			name:        "empty recipes file",
			filename:    "empty.json",
			data:        []byte(`{"recipes": []}`),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.filename)

			// Create the test file
			err := os.WriteFile(filePath, tt.data, 0644)
			require.NoError(t, err)

			recipeState := NewFileRecipeState(filePath)
			loadedData, err := recipeState.Load(context.Background())

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.data, loadedData)
		})
	}

	t.Run("load nonexistent file", func(t *testing.T) {
		nonexistentPath := filepath.Join(tmpDir, "nonexistent.json")
		recipeState := NewFileRecipeState(nonexistentPath)
		_, err := recipeState.Load(context.Background())
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})
}
