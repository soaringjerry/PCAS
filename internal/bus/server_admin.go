package bus

import (
    "fmt"
    "log"
    "os"

    "google.golang.org/protobuf/types/known/structpb"

    eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
    "github.com/soaringjerry/pcas/internal/policy"
)

// handleAdminEvent intercepts admin control events
func (s *Server) handleAdminEvent(event *eventsv1.Event) (handled bool, err error) {
    switch event.Type {
    case "pcas.admin.policy.add_rule.v1":
        return true, s.handleAdminPolicyAddRule(event)
    default:
        return false, nil
    }
}

func (s *Server) handleAdminPolicyAddRule(event *eventsv1.Event) error {
    // Authorization via admin token
    expected := os.Getenv("PCAS_ADMIN_TOKEN")
    if expected != "" {
        token := ""
        if event.Attributes != nil {
            token = event.Attributes["admin_token"]
        }
        if token == "" || token != expected {
            return fmt.Errorf("unauthorized admin request: missing or invalid admin_token")
        }
    }

    // Parse payload from event.Data (structpb.Value expected)
    if event.Data == nil {
        return fmt.Errorf("missing data for admin add_rule")
    }
    value := &structpb.Value{}
    if !event.Data.MessageIs(value) {
        return fmt.Errorf("admin payload must be a struct (structpb.Value)")
    }
    if err := event.Data.UnmarshalTo(value); err != nil {
        return fmt.Errorf("failed to parse admin payload: %w", err)
    }
    m, ok := value.AsInterface().(map[string]interface{})
    if !ok {
        return fmt.Errorf("invalid payload type")
    }

    // Extract fields
    getStr := func(k string) string {
        if v, ok := m[k]; ok {
            if s, ok := v.(string); ok {
                return s
            }
        }
        return ""
    }
    eventType := getStr("event_type")
    provider := getStr("provider")
    promptTpl := getStr("prompt_template")
    name := getStr("name")
    if eventType == "" || provider == "" {
        return fmt.Errorf("event_type and provider are required")
    }
    if name == "" {
        name = fmt.Sprintf("route-%s-to-%s", eventType, provider)
    }

    // Update in-memory policy
    r := policy.Rule{
        Name: name,
        If:   policy.Condition{EventType: eventType},
        Then: policy.Action{Provider: provider, PromptTemplate: promptTpl},
    }
    s.policyEngine.AddRule(r)

    // Persist dynamic policy to data volume so it's writable by the container user
    if err := policy.SavePolicy("/data/policy.yaml", s.policyEngine.Policy()); err != nil {
        log.Printf("[ADMIN] added rule in memory but failed to persist to /data/policy.yaml: %v", err)
        return fmt.Errorf("failed to persist policy: %w", err)
    }

    log.Printf("[ADMIN] Added policy rule: %s (event_type=%s -> provider=%s)", name, eventType, provider)
    return nil
}
