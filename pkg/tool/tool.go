package tool

type Tool struct {
	Name          string
	Description   string
	Parameters    map[string]any
	Run           func(args string) (string, error)
	NeedsApproval bool // if true, agent ask user before executing
}

type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name] = t
}

// Get returns the tool with the given name if it exists.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns a copy of the registry's tools.'
func (r *Registry) List() []Tool {
	var tools []Tool
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}
