package tools

import (
	"fmt"

	"pantryagent/tools/storage"
)

// Registry maps tool names to implementations
type Registry map[string]Tool

// NewRegistry creates a new tool registry with the given pantry and recipe states.
func NewRegistry(pantry storage.PantryState, recipes storage.RecipeState) (*Registry, error) {
	tools := map[string]Tool{
		"pantry_get": NewPantryGet(pantry),
		"recipe_get": NewRecipeGet(recipes),
	}

	registry := Registry(tools)
	return &registry, nil
}

// GetTools returns all tools in the registry as a slice
func (r *Registry) GetTools() []Tool {
	tools := make([]Tool, 0, len(*r))
	for _, tool := range *r {
		tools = append(tools, tool)
	}
	return tools
}

// GetTool retrieves a tool by name from the registry
func (r Registry) GetTool(name string) (Tool, error) {
	tool, exists := r[name]
	if !exists {
		return nil, fmt.Errorf("tool %q not found in registry", name)
	}
	return tool, nil
}
