package policy

import (
	"testing"

	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

func TestSelectProvider_AnyOf(t *testing.T) {
	// 构造包含 any_of 规则的策略
	policy := &Policy{
		Version: "v1",
		Providers: []ProviderConfig{
			{
				Name: "mock-provider",
				Type: "mock",
			},
			{
				Name: "openai-gpt4",
				Type: "openai",
			},
		},
		Rules: []Rule{
			{
				Name: "Rule for PCAS domain events",
				If: Condition{
					AnyOf: []Condition{
						{EventType: "pcas.architect.decision.v1"},
						{EventType: "pcas.schedule.item.v1"},
						{EventType: "pcas.plan.trip.v1"},
						{EventType: "pcas.memory.create.v1"},
					},
				},
				Then: Action{
					Provider: "mock-provider",
				},
			},
			{
				Name: "Rule for user prompts",
				If: Condition{
					EventType: "pcas.user.prompt.v1",
				},
				Then: Action{
					Provider: "openai-gpt4",
				},
			},
		},
	}

	// 创建引擎实例
	engine := &Engine{
		policy: policy,
	}

	// 定义测试用例
	testCases := []struct {
		name             string
		event            *eventsv1.Event
		expectedProvider string
		expectError      bool
	}{
		{
			name: "matches first any_of condition",
			event: &eventsv1.Event{
				Type: "pcas.architect.decision.v1",
			},
			expectedProvider: "mock-provider",
			expectError:      false,
		},
		{
			name: "matches second any_of condition",
			event: &eventsv1.Event{
				Type: "pcas.schedule.item.v1",
			},
			expectedProvider: "mock-provider",
			expectError:      false,
		},
		{
			name: "matches third any_of condition",
			event: &eventsv1.Event{
				Type: "pcas.plan.trip.v1",
			},
			expectedProvider: "mock-provider",
			expectError:      false,
		},
		{
			name: "matches fourth any_of condition",
			event: &eventsv1.Event{
				Type: "pcas.memory.create.v1",
			},
			expectedProvider: "mock-provider",
			expectError:      false,
		},
		{
			name: "matches simple event_type condition",
			event: &eventsv1.Event{
				Type: "pcas.user.prompt.v1",
			},
			expectedProvider: "openai-gpt4",
			expectError:      false,
		},
		{
			name: "no matching rule",
			event: &eventsv1.Event{
				Type: "unknown.event.type",
			},
			expectedProvider: "",
			expectError:      true,
		},
	}

	// 执行测试
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := engine.SelectProvider(tc.event)
			
			// 检查返回的 provider
			if tc.expectError && provider != "" {
				t.Errorf("expected empty provider for no match, got %q", provider)
			} else if !tc.expectError && provider != tc.expectedProvider {
				t.Errorf("expected provider %q, got %q", tc.expectedProvider, provider)
			}
		})
	}
}

