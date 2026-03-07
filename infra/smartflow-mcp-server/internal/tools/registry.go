package tools

import (
	"context"
	"fmt"
	"sort"
)

type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(toolList ...Tool) (*Registry, error) {
	r := &Registry{tools: make(map[string]Tool, len(toolList))}
	for _, t := range toolList {
		name := t.Name()
		if _, exists := r.tools[name]; exists {
			return nil, fmt.Errorf("duplicated tool name: %s", name)
		}
		r.tools[name] = t
	}
	return r, nil
}

func (r *Registry) Find(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := r.tools[name]
		out = append(out, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"inputSchema": t.InputSchema(),
		})
	}
	return out
}
