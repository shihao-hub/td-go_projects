package metadata

// Metadata represents the standardized temporal anti-corrosion metadata for KB entries.
type Metadata struct {
	ID                   string         `yaml:"id,omitempty"`
	Domain               string         `yaml:"domain,omitempty"`
	Scope                string         `yaml:"scope,omitempty"`
	Type                 string         `yaml:"type,omitempty"`
	Slug                 string         `yaml:"slug,omitempty"`
	Status               string         `yaml:"status,omitempty"` // verified | draft | suspect | deprecated
	Created              string         `yaml:"created,omitempty"`
	LastVerified         string         `yaml:"last_verified,omitempty"`
	SupersededBy         *string        `yaml:"superseded_by,omitempty"`
	DeprecationReason    *string        `yaml:"deprecation_reason,omitempty"`
	Compatibility        *Compatibility `yaml:"compatibility,omitempty"`
	FalsificationSignals any            `yaml:"falsification_signals,omitempty"` // Supports []string or complex objects
	Invocation           *Invocation    `yaml:"invocation,omitempty"`
	Name                 string         `yaml:"name,omitempty"`
	Description          string         `yaml:"description,omitempty"`
	Extra                map[string]any `yaml:",inline"`
}

type Compatibility struct {
	OS    []string       `yaml:"os,omitempty"`
	Tools map[string]any `yaml:"tools,omitempty"`
}

type Invocation struct {
	AutoTrigger     bool     `yaml:"auto_trigger"`
	TriggerKeywords []string `yaml:"trigger_keywords,omitempty"`
}

const (
	StatusVerified   = "verified"
	StatusDraft      = "draft"
	StatusSuspect    = "suspect"
	StatusDeprecated = "deprecated"
)
