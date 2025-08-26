package storage

import (
	"context"
	"os"
)

type FilePantryState struct {
	FilePath string
}

func NewFilePantryState(filePath string) *FilePantryState {
	return &FilePantryState{FilePath: filePath}
}

func (p *FilePantryState) Load(ctx context.Context) ([]byte, error) {
	return os.ReadFile(p.FilePath)
}

type FileRecipeState struct {
	FilePath string
}

func NewFileRecipeState(filePath string) *FileRecipeState {
	return &FileRecipeState{FilePath: filePath}
}

func (r *FileRecipeState) Load(ctx context.Context) ([]byte, error) {
	return os.ReadFile(r.FilePath)
}
