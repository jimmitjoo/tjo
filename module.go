package gemquick

import (
	"context"
	"fmt"
	"sync"
)

// Module defines the interface for optional framework components.
// Modules are initialized after core services and can depend on them.
// Use this for opt-in features like SMS, Email, WebSockets, OpenTelemetry, etc.
type Module interface {
	// Name returns a unique identifier for this module (e.g., "sms", "email", "websocket")
	Name() string

	// Initialize sets up the module with access to the framework.
	// Called during app.New() after core services are ready.
	Initialize(g *Gemquick) error

	// Shutdown gracefully stops the module.
	// Called during graceful shutdown with a context for timeout control.
	Shutdown(ctx context.Context) error
}

// ModuleRegistry manages module registration and lifecycle.
// It ensures modules are initialized in registration order and
// shut down in reverse order.
type ModuleRegistry struct {
	modules map[string]Module
	order   []string // Preserves registration order
	mu      sync.RWMutex
}

// NewModuleRegistry creates an empty module registry.
func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{
		modules: make(map[string]Module),
		order:   make([]string, 0),
	}
}

// Register adds a module to the registry.
// Modules are initialized in the order they are registered.
// Returns an error if a module with the same name already exists.
func (r *ModuleRegistry) Register(m Module) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := m.Name()
	if _, exists := r.modules[name]; exists {
		return fmt.Errorf("module %q already registered", name)
	}

	r.modules[name] = m
	r.order = append(r.order, name)
	return nil
}

// Get returns a module by name, or nil if not found.
func (r *ModuleRegistry) Get(name string) Module {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modules[name]
}

// Has checks if a module is registered.
func (r *ModuleRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.modules[name]
	return ok
}

// Names returns all registered module names in registration order.
func (r *ModuleRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// InitializeAll calls Initialize on all registered modules in order.
// Stops on first error and returns it.
func (r *ModuleRegistry) InitializeAll(g *Gemquick) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range r.order {
		m := r.modules[name]
		if err := m.Initialize(g); err != nil {
			return fmt.Errorf("module %q initialization failed: %w", name, err)
		}
	}
	return nil
}

// ShutdownAll calls Shutdown on all registered modules in reverse order.
// Collects all errors and returns them combined.
func (r *ModuleRegistry) ShutdownAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errs []error

	// Shutdown in reverse order
	for i := len(r.order) - 1; i >= 0; i-- {
		name := r.order[i]
		m := r.modules[name]
		if err := m.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("module %q shutdown failed: %w", name, err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	// Combine errors
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple shutdown errors:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

// Count returns the number of registered modules.
func (r *ModuleRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.modules)
}
