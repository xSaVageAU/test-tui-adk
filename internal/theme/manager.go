package theme

// Manager holds the set of themes registered with the app and tracks which
// one is active. It's the single mutation point for "swap the theme" — UI
// code should call Manager.Next/Set and re-pull Styles(), never hold onto a
// Styles value across a theme change.
type Manager struct {
	themes []Theme
	index  int
}

// NewManager builds a Manager over the given themes, starting on the first
// one. Panics if themes is empty since a themeless app has no valid state.
func NewManager(themes ...Theme) *Manager {
	if len(themes) == 0 {
		panic("theme: NewManager requires at least one theme")
	}
	return &Manager{themes: themes}
}

// Current returns the active theme.
func (m *Manager) Current() Theme {
	return m.themes[m.index]
}

// Styles compiles the active theme into a Styles set.
func (m *Manager) Styles() Styles {
	return New(m.Current())
}

// Next advances to the next registered theme, wrapping around.
func (m *Manager) Next() Theme {
	m.index = (m.index + 1) % len(m.themes)
	return m.Current()
}

// Prev moves to the previous registered theme, wrapping around.
func (m *Manager) Prev() Theme {
	m.index = (m.index - 1 + len(m.themes)) % len(m.themes)
	return m.Current()
}

// Set switches to the theme with the given name. No-op if not found.
func (m *Manager) Set(name string) {
	for i, t := range m.themes {
		if t.Name == name {
			m.index = i
			return
		}
	}
}

// Names returns the registered theme names in order.
func (m *Manager) Names() []string {
	names := make([]string, len(m.themes))
	for i, t := range m.themes {
		names[i] = t.Name
	}
	return names
}
