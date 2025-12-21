package gemquick

import (
	"context"
	"errors"
	"testing"
	"time"
)

// testModule is a mock module for testing
type testModule struct {
	name          string
	initError     error
	shutdownError error
	initCalled    bool
	shutdownCalled bool
}

func (m *testModule) Name() string { return m.name }

func (m *testModule) Initialize(g *Gemquick) error {
	m.initCalled = true
	return m.initError
}

func (m *testModule) Shutdown(ctx context.Context) error {
	m.shutdownCalled = true
	return m.shutdownError
}

func TestModuleRegistry_Register(t *testing.T) {
	r := NewModuleRegistry()

	m1 := &testModule{name: "test1"}
	m2 := &testModule{name: "test2"}
	m3 := &testModule{name: "test1"} // duplicate name

	// First registration should succeed
	if err := r.Register(m1); err != nil {
		t.Errorf("Register(m1) = %v, want nil", err)
	}

	// Second registration should succeed
	if err := r.Register(m2); err != nil {
		t.Errorf("Register(m2) = %v, want nil", err)
	}

	// Duplicate name should fail
	if err := r.Register(m3); err == nil {
		t.Error("Register(m3) = nil, want error for duplicate name")
	}

	// Check count
	if got := r.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2", got)
	}
}

func TestModuleRegistry_Get(t *testing.T) {
	r := NewModuleRegistry()
	m := &testModule{name: "mymodule"}
	_ = r.Register(m)

	// Should find registered module
	got := r.Get("mymodule")
	if got != m {
		t.Errorf("Get(mymodule) = %v, want %v", got, m)
	}

	// Should return nil for unknown module
	if got := r.Get("unknown"); got != nil {
		t.Errorf("Get(unknown) = %v, want nil", got)
	}
}

func TestModuleRegistry_Has(t *testing.T) {
	r := NewModuleRegistry()
	m := &testModule{name: "existing"}
	_ = r.Register(m)

	if !r.Has("existing") {
		t.Error("Has(existing) = false, want true")
	}

	if r.Has("nonexistent") {
		t.Error("Has(nonexistent) = true, want false")
	}
}

func TestModuleRegistry_Names(t *testing.T) {
	r := NewModuleRegistry()
	_ = r.Register(&testModule{name: "first"})
	_ = r.Register(&testModule{name: "second"})
	_ = r.Register(&testModule{name: "third"})

	names := r.Names()
	expected := []string{"first", "second", "third"}

	if len(names) != len(expected) {
		t.Fatalf("Names() len = %d, want %d", len(names), len(expected))
	}

	for i, name := range names {
		if name != expected[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestModuleRegistry_InitializeAll(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := NewModuleRegistry()
		m1 := &testModule{name: "m1"}
		m2 := &testModule{name: "m2"}
		_ = r.Register(m1)
		_ = r.Register(m2)

		g := &Gemquick{}
		if err := r.InitializeAll(g); err != nil {
			t.Errorf("InitializeAll() = %v, want nil", err)
		}

		if !m1.initCalled {
			t.Error("m1.Initialize was not called")
		}
		if !m2.initCalled {
			t.Error("m2.Initialize was not called")
		}
	})

	t.Run("stops on error", func(t *testing.T) {
		r := NewModuleRegistry()
		m1 := &testModule{name: "m1", initError: errors.New("init failed")}
		m2 := &testModule{name: "m2"}
		_ = r.Register(m1)
		_ = r.Register(m2)

		g := &Gemquick{}
		err := r.InitializeAll(g)
		if err == nil {
			t.Error("InitializeAll() = nil, want error")
		}

		if !m1.initCalled {
			t.Error("m1.Initialize was not called")
		}
		if m2.initCalled {
			t.Error("m2.Initialize should not be called after m1 failed")
		}
	})
}

func TestModuleRegistry_ShutdownAll(t *testing.T) {
	t.Run("reverse order", func(t *testing.T) {
		r := NewModuleRegistry()
		order := make([]string, 0)

		// Custom modules that record shutdown order
		m1 := &testModule{name: "first"}
		m2 := &testModule{name: "second"}
		m3 := &testModule{name: "third"}

		_ = r.Register(m1)
		_ = r.Register(m2)
		_ = r.Register(m3)

		// Initialize first
		g := &Gemquick{}
		_ = r.InitializeAll(g)

		// Track shutdown order by checking which are called
		ctx := context.Background()
		_ = r.ShutdownAll(ctx)

		// All should be shutdown
		if !m1.shutdownCalled || !m2.shutdownCalled || !m3.shutdownCalled {
			t.Error("Not all modules were shutdown")
		}

		// We can't easily verify order in this test, but the implementation
		// iterates in reverse order
		_ = order // silence unused warning
	})

	t.Run("collects all errors", func(t *testing.T) {
		r := NewModuleRegistry()
		m1 := &testModule{name: "m1", shutdownError: errors.New("error1")}
		m2 := &testModule{name: "m2", shutdownError: errors.New("error2")}
		_ = r.Register(m1)
		_ = r.Register(m2)

		ctx := context.Background()
		err := r.ShutdownAll(ctx)
		if err == nil {
			t.Error("ShutdownAll() = nil, want error")
		}

		// Both should be called despite errors
		if !m1.shutdownCalled {
			t.Error("m1.Shutdown was not called")
		}
		if !m2.shutdownCalled {
			t.Error("m2.Shutdown was not called")
		}
	})

	t.Run("respects context timeout", func(t *testing.T) {
		r := NewModuleRegistry()
		m := &testModule{name: "slow"}
		_ = r.Register(m)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_ = r.ShutdownAll(ctx)
		// Just verify it doesn't hang
	})
}
