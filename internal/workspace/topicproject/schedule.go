package topicproject

import (
	"context"
	"sort"
)

// ScheduleUnit reads and queues the topic project checks of one owner. Each
// method reads or writes; none decides.
type ScheduleUnit interface {
	// Topics are the groups that may be eligible: enough current memories,
	// and named a project or holding a goal with a date.
	Topics(ctx context.Context) ([]string, error)
	// Input is the topic's current input; false when the topic is gone or
	// not Eligible.
	Input(ctx context.Context, topic string) (Input, bool, error)
	// Answered is true when the topic was linked to a project for this
	// evidence, or judged not to be one.
	Answered(ctx context.Context, topic, evidence string) (bool, error)
	// Queue queues the first page of the check, as the order-th of this pass;
	// false when it is queued already.
	Queue(ctx context.Context, topic string, input Input, order int) (bool, error)
	// Overflow records the count of what did not fit, by reason.
	Overflow(ctx context.Context, reason string, count int) error
}

// Schedule queues a check of each eligible topic not answered for its
// evidence. Only a few projects are made a day and two groups are judged an
// hour, so what was spoken of last goes first. A queued check counts the
// memories and characters its input leaves out. It gives the count queued.
func Schedule(ctx context.Context, u ScheduleUnit) (int, error) {
	topics, err := u.Topics(ctx)
	if err != nil {
		return 0, err
	}
	type due struct {
		topic string
		input Input
	}
	dues := []due{}
	for _, topic := range topics {
		input, ok, err := u.Input(ctx, topic)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		answered, err := u.Answered(ctx, topic, input.EvidenceHash)
		if err != nil {
			return 0, err
		}
		if !answered {
			dues = append(dues, due{topic, input})
		}
	}
	sort.SliceStable(dues, func(i, j int) bool { return dues[i].input.Latest.After(dues[j].input.Latest) })
	count := 0
	for i, d := range dues {
		queued, err := u.Queue(ctx, d.topic, d.input, i)
		if err != nil {
			return count, err
		}
		if !queued {
			continue
		}
		count++
		if d.input.Count > EvidenceLimit {
			if err := u.Overflow(ctx, "topic_project_evidence_left_in_group", d.input.Count-EvidenceLimit); err != nil {
				return count, err
			}
		}
		if d.input.ClippedCharacters > 0 {
			if err := u.Overflow(ctx, "topic_project_characters_left_in_group", d.input.ClippedCharacters); err != nil {
				return count, err
			}
		}
	}
	return count, nil
}
