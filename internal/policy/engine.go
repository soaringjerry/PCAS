package policy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

// Policy represents the entire policy configuration
type Policy struct {
	Version   string           `yaml:"version"`
	Providers []ProviderConfig `yaml:"providers"`
	Rules     []Rule          `yaml:"rules"`
}

// ProviderConfig represents a provider configuration
type ProviderConfig struct {
	Name string                 `yaml:"name"`
	Type string                 `yaml:"type"`
	Config map[string]interface{} `yaml:",inline"`
}

// Rule represents a single policy rule
type Rule struct {
	Name string    `yaml:"name"`
	If   Condition `yaml:"if"`
	Then Action    `yaml:"then"`
}

// Condition represents the condition part of a rule
type Condition struct {
	EventType string      `yaml:"event_type"`
	AnyOf     []Condition `yaml:"any_of"`
}

// Action represents the action part of a rule
type Action struct {
	Provider string `yaml:"provider"`
}

// Engine is the policy evaluation engine
type Engine struct {
	policy *Policy
}

// NewEngine creates a new policy engine with the given policy
func NewEngine(policy *Policy) *Engine {
	return &Engine{
		policy: policy,
	}
}

// LoadPolicy loads a policy from a YAML file
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}

	var policy Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("failed to parse policy file: %w", err)
	}

	return &policy, nil
}

// SelectProvider selects a provider based on the event type
func (e *Engine) SelectProvider(event *eventsv1.Event) string {
	for _, rule := range e.policy.Rules {
		// Step 1: Check direct event_type match (backward compatibility)
		if rule.If.EventType != "" && rule.If.EventType == event.Type {
			return rule.Then.Provider
		}
		
		// Step 2: Check any_of conditions
		if len(rule.If.AnyOf) > 0 {
			for _, condition := range rule.If.AnyOf {
				if condition.EventType == event.Type {
					return rule.Then.Provider
				}
			}
		}
	}
	
	// Return empty string if no matching rule found
	return ""
}