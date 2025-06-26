package bus

import (
	"testing"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	
	eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
)

func TestExtractTextContent(t *testing.T) {
	// 创建一个 Server 实例用于测试
	s := &Server{}

	testCases := []struct {
		name     string
		event    *eventsv1.Event
		expected string
	}{
		{
			name: "Subject has content, Data is empty",
			event: &eventsv1.Event{
				Type:    "test.event.v1",
				Subject: "This is the subject content",
				Data:    nil,
			},
			expected: "This is the subject content",
		},
		{
			name: "Subject is empty, Data contains text field",
			event: func() *eventsv1.Event {
				data := &structpb.Value{
					Kind: &structpb.Value_StructValue{
						StructValue: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"content": {
									Kind: &structpb.Value_StringValue{
										StringValue: "This is from data field",
									},
								},
							},
						},
					},
				}
				anyData, _ := anypb.New(data)
				return &eventsv1.Event{
					Type:    "test.event.v1",
					Subject: "",
					Data:    anyData,
				}
			}(),
			expected: "This is from data field",
		},
		{
			name: "Both Subject and Data have content - Subject takes priority",
			event: func() *eventsv1.Event {
				data := &structpb.Value{
					Kind: &structpb.Value_StructValue{
						StructValue: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"content": {
									Kind: &structpb.Value_StringValue{
										StringValue: "This is from data field",
									},
								},
							},
						},
					},
				}
				anyData, _ := anypb.New(data)
				return &eventsv1.Event{
					Type:    "test.event.v1",
					Subject: "Subject takes priority",
					Data:    anyData,
				}
			}(),
			expected: "Subject takes priority",
		},
		{
			name: "Both Subject and Data are empty",
			event: &eventsv1.Event{
				Type:    "test.event.v1",
				Subject: "",
				Data:    nil,
			},
			expected: "",
		},
		{
			name: "Data contains multiple text fields",
			event: func() *eventsv1.Event {
				data := &structpb.Value{
					Kind: &structpb.Value_StructValue{
						StructValue: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"prompt": {
									Kind: &structpb.Value_StringValue{
										StringValue: "This is a prompt",
									},
								},
								"message": {
									Kind: &structpb.Value_StringValue{
										StringValue: "This is a message",
									},
								},
							},
						},
					},
				}
				anyData, _ := anypb.New(data)
				return &eventsv1.Event{
					Type:    "test.event.v1",
					Subject: "",
					Data:    anyData,
				}
			}(),
			expected: "This is a prompt This is a message",
		},
		{
			name: "Data contains non-text fields",
			event: func() *eventsv1.Event {
				data := &structpb.Value{
					Kind: &structpb.Value_StructValue{
						StructValue: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"count": {
									Kind: &structpb.Value_NumberValue{
										NumberValue: 42,
									},
								},
								"enabled": {
									Kind: &structpb.Value_BoolValue{
										BoolValue: true,
									},
								},
							},
						},
					},
				}
				anyData, _ := anypb.New(data)
				return &eventsv1.Event{
					Type:    "test.event.v1",
					Subject: "",
					Data:    anyData,
				}
			}(),
			expected: "{\"count\":42,\"enabled\":true}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := s.extractTextContent(tc.event)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestExtractTextContent_InvalidData(t *testing.T) {
	s := &Server{}

	// 测试无法解析的 Data
	event := &eventsv1.Event{
		Type:    "test.event.v1",
		Subject: "",
		Data: &anypb.Any{
			TypeUrl: "type.googleapis.com/invalid.Type",
			Value:   []byte("invalid data"),
		},
	}

	result := s.extractTextContent(event)
	if result != "" {
		t.Errorf("expected empty string for invalid data, got %q", result)
	}
}