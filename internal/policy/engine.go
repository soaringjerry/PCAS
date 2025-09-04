package policy

import (
    "fmt"
    "os"
    "sync"

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
	Provider       string `yaml:"provider"`
	PromptTemplate string `yaml:"prompt_template,omitempty"`
}

// Engine is the policy evaluation engine
type Engine struct {
    policy *Policy
    mu     sync.RWMutex
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

// SavePolicy persists the policy to a YAML file
func SavePolicy(path string, p *Policy) error {
    data, err := yaml.Marshal(p)
    if err != nil {
        return fmt.Errorf("failed to marshal policy: %w", err)
    }
    if err := os.WriteFile(path, data, 0644); err != nil {
        return fmt.Errorf("failed to write policy file: %w", err)
    }
    return nil
}

// SelectProvider selects a provider based on the event type
func (e *Engine) SelectProvider(event *eventsv1.Event) (string, string) {
    e.mu.RLock()
    defer e.mu.RUnlock()
    for _, rule := range e.policy.Rules {
        // Step 1: Check direct event_type match (backward compatibility)
        if rule.If.EventType != "" && rule.If.EventType == event.Type {
            return rule.Then.Provider, rule.Then.PromptTemplate
        }
		
		// Step 2: Check any_of conditions
		if len(rule.If.AnyOf) > 0 {
			for _, condition := range rule.If.AnyOf {
				if condition.EventType == event.Type {
					return rule.Then.Provider, rule.Then.PromptTemplate
				}
			}
		}
	}
	
	// Return empty string if no matching rule found
    return "", ""
}

// SelectProviderForStream selects a provider for streaming based on the event type
func (e *Engine) SelectProviderForStream(eventType string) (string, string) {
    // For now, use the same logic as SelectProvider
    // In the future, we might want to add specific streaming provider configuration
    e.mu.RLock()
    defer e.mu.RUnlock()
    for _, rule := range e.policy.Rules {
        // Step 1: Check direct event_type match (backward compatibility)
        if rule.If.EventType != "" && rule.If.EventType == eventType {
            return rule.Then.Provider, rule.Then.PromptTemplate
        }
		
		// Step 2: Check any_of conditions
		if len(rule.If.AnyOf) > 0 {
			for _, condition := range rule.If.AnyOf {
				if condition.EventType == eventType {
					return rule.Then.Provider, rule.Then.PromptTemplate
				}
			}
		}
	}
	
	// Return empty string if no matching rule found
    return "", ""
}

// AddRule appends a new rule to the in-memory policy (thread-safe)
func (e *Engine) AddRule(r Rule) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.policy.Rules = append(e.policy.Rules, r)
}

// Policy returns a copy of the current policy pointer (read-only usage)
func (e *Engine) Policy() *Policy {
    e.mu.RLock()
    defer e.mu.RUnlock()
    return e.policy
}
