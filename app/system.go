package app

import "sync/atomic"

// SystemFunc is the signature every system function must satisfy. It is a type
// alias so plain func(*Ctx) literals are accepted wherever SystemFunc is.
type SystemFunc = func(*Ctx)

// SystemConfig wraps a system function with optional ordering and run-condition
// metadata. Use System(fn) to build one and chain configuration.
type SystemConfig struct {
	Fn     SystemFunc
	label  string
	runIf  []func(*Ctx) bool
	before []string
	after  []string
}

// System wraps fn for advanced configuration.
func System(fn SystemFunc) *SystemConfig {
	return &SystemConfig{Fn: fn}
}

// Label assigns a label that other systems can refer to via Before/After.
// Labels must be unique within a single stage.
func (s *SystemConfig) Label(name string) *SystemConfig {
	s.label = name
	return s
}

// GetLabel returns the label assigned via Label, or "" if none. Interceptors
// (see App.SetSystemInterceptor) use it to identify systems.
func (s *SystemConfig) GetLabel() string { return s.label }

// Before declares this system must run before each of the labeled systems.
func (s *SystemConfig) Before(labels ...string) *SystemConfig {
	s.before = append(s.before, labels...)
	return s
}

// After declares this system must run after each of the labeled systems.
func (s *SystemConfig) After(labels ...string) *SystemConfig {
	s.after = append(s.after, labels...)
	return s
}

// RunIf adds a run condition. The system runs only when every condition
// returns true; multiple RunIf calls AND together.
func (s *SystemConfig) RunIf(cond func(*Ctx) bool) *SystemConfig {
	s.runIf = append(s.runIf, cond)
	return s
}

var autoLabelCounter uint64

func nextAutoLabel() string {
	n := atomic.AddUint64(&autoLabelCounter, 1)
	return autoLabelPrefix + uintToStr(n)
}

const autoLabelPrefix = "__spliti_auto_"

func uintToStr(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Chain wires sequential .After edges across the given configs so they run in
// the order provided. Configs without an explicit Label get an auto-generated
// one. The returned slice can be spread into AddSystems.
func Chain(configs ...*SystemConfig) []*SystemConfig {
	for _, c := range configs {
		if c.label == "" {
			c.label = nextAutoLabel()
		}
	}
	for i := 1; i < len(configs); i++ {
		configs[i].after = append(configs[i].after, configs[i-1].label)
	}
	return configs
}
