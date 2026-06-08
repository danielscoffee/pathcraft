// Package registry holds compile-time plugin registrations: algorithms,
// graph loaders, exporters, and cost models. Plugins register themselves
// in init() and CLI/library users blank-import the plugin packages they want.
package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

// Registry is the lookup table. Methods are safe for concurrent reads after
// registration; registration itself is expected at init time.
type Registry struct {
	mu        sync.RWMutex
	algos     map[string]core.Algorithm
	loaders   map[string]core.GraphLoader
	exporters map[string]core.Exporter
	costs     map[string]core.CostModel
}

// New returns an empty Registry. Use this for isolated tests; production
// code typically uses Default.
func New() *Registry {
	return &Registry{
		algos:     make(map[string]core.Algorithm),
		loaders:   make(map[string]core.GraphLoader),
		exporters: make(map[string]core.Exporter),
		costs:     make(map[string]core.CostModel),
	}
}

// Default is the process-wide registry used by plugin init() functions
// and the CLI.
var Default = New()

// ErrDuplicate is returned by Register* when a plugin with the same Name()
// is already registered.
type ErrDuplicate struct {
	Kind string
	Name string
}

func (e ErrDuplicate) Error() string {
	return fmt.Sprintf("registry: duplicate %s %q", e.Kind, e.Name)
}

func (r *Registry) RegisterAlgorithm(a core.Algorithm) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.algos[a.Name()]; ok {
		return ErrDuplicate{Kind: "algorithm", Name: a.Name()}
	}
	r.algos[a.Name()] = a
	return nil
}

func (r *Registry) RegisterLoader(l core.GraphLoader) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.loaders[l.Name()]; ok {
		return ErrDuplicate{Kind: "loader", Name: l.Name()}
	}
	r.loaders[l.Name()] = l
	return nil
}

func (r *Registry) RegisterExporter(e core.Exporter) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exporters[e.Name()]; ok {
		return ErrDuplicate{Kind: "exporter", Name: e.Name()}
	}
	r.exporters[e.Name()] = e
	return nil
}

func (r *Registry) RegisterCostModel(c core.CostModel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.costs[c.Name()]; ok {
		return ErrDuplicate{Kind: "cost", Name: c.Name()}
	}
	r.costs[c.Name()] = c
	return nil
}

func (r *Registry) Algorithm(name string) (core.Algorithm, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.algos[name]
	return a, ok
}

func (r *Registry) Loader(name string) (core.GraphLoader, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.loaders[name]
	return l, ok
}

func (r *Registry) Exporter(name string) (core.Exporter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.exporters[name]
	return e, ok
}

func (r *Registry) CostModel(name string) (core.CostModel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.costs[name]
	return c, ok
}

func (r *Registry) Algorithms() []string { return sortedKeys(r.algos, &r.mu) }
func (r *Registry) Loaders() []string    { return sortedLoaderKeys(r.loaders, &r.mu) }
func (r *Registry) Exporters() []string  { return sortedExporterKeys(r.exporters, &r.mu) }
func (r *Registry) CostModels() []string { return sortedCostKeys(r.costs, &r.mu) }

func sortedKeys(m map[string]core.Algorithm, mu *sync.RWMutex) []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedLoaderKeys(m map[string]core.GraphLoader, mu *sync.RWMutex) []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedExporterKeys(m map[string]core.Exporter, mu *sync.RWMutex) []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCostKeys(m map[string]core.CostModel, mu *sync.RWMutex) []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MustRegisterAlgorithm panics on duplicate. Intended for plugin init().
func MustRegisterAlgorithm(a core.Algorithm) {
	if err := Default.RegisterAlgorithm(a); err != nil {
		panic(err)
	}
}

// MustRegisterLoader panics on duplicate. Intended for plugin init().
func MustRegisterLoader(l core.GraphLoader) {
	if err := Default.RegisterLoader(l); err != nil {
		panic(err)
	}
}

// MustRegisterExporter panics on duplicate. Intended for plugin init().
func MustRegisterExporter(e core.Exporter) {
	if err := Default.RegisterExporter(e); err != nil {
		panic(err)
	}
}

// MustRegisterCostModel panics on duplicate. Intended for plugin init().
func MustRegisterCostModel(c core.CostModel) {
	if err := Default.RegisterCostModel(c); err != nil {
		panic(err)
	}
}
