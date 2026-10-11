package extraction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

const (
	// ConversationPrefix starts the stage of a conversation segment job.
	ConversationPrefix = "source.extract:conversation:"
	// ConversationVersion is the rule version of conversation extraction.
	ConversationVersion = 4
	// segmentCharacters is the length of one segment, in characters.
	segmentCharacters = 12000
)

// ConversationMessage is one message of an imported conversation, or one part
// of a long message.
type ConversationMessage struct {
	Index       int        `json:"message_index"`
	Role        string     `json:"role"`
	Text        string     `json:"text"`
	ExpressedAt *time.Time `json:"expressed_at"`
	Part        int        `json:"part,omitempty"`
	Parts       int        `json:"parts,omitempty"`
}

// ConversationSegment is the input of one model call: the messages to read
// and the messages before them.
type ConversationSegment struct {
	Messages []ConversationMessage
	Context  []ConversationMessage
}

// ConversationItem is an item with the message that it comes from.
type ConversationItem struct {
	Item
	MessageIndex int `json:"message_index"`
}

// ConversationOutput is the model's answer for one segment. Withdraw names
// earlier statements of the run that a later message corrects.
type ConversationOutput struct {
	Items    []ConversationItem `json:"items"`
	Withdraw []int              `json:"withdraw,omitempty"`
}

// ParseConversationOutput reads the model's answer with the rules of ParseOutput.
func ParseConversationOutput(text string) (ConversationOutput, bool) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var out ConversationOutput
	if modelcall.DecodeStrict([]byte(strings.TrimSpace(text)), &out) != nil {
		return ConversationOutput{}, false
	}
	return out, true
}

// CurrentBranch returns the messages that are not on a historical branch.
func CurrentBranch(sources []memory.SourceResult) []memory.SourceResult {
	out := []memory.SourceResult{}
	for _, source := range sources {
		if source.Context.Branch != "historical" {
			out = append(out, source)
		}
	}
	return out
}

func fenced(v any) []byte { b, _ := json.Marshal(v); return b }

// DecideConversation applies the rules of one item of a segment. Only a
// user's message of the segment gives an item, the quotation must be in that
// message, and each statement waits for review. grounding is the text of the
// segment and its context.
func DecideConversation(item ConversationItem, messages map[int]ConversationMessage, sources []memory.SourceResult, grounding string, location *time.Location) (Decision, memory.SourceResult) {
	message, ok := messages[item.MessageIndex]
	if !ok || message.Role != "user" || item.Quote == "" || !strings.Contains(message.Text, item.Quote) || strings.TrimSpace(item.Text) == "" || len(item.Text) > 1<<20 || !oneOf(item.Kind, "memory", "task", "idea") || item.Kind == "memory" && !Statement(item.Item) || item.Confidence < 0 || item.Confidence > 1 {
		return Decision{Item: item.Item}, memory.SourceResult{}
	}
	if !oneOf(item.Acquisition, "direct", "reported", "inferred") {
		item.Acquisition = "inferred"
	}
	source := sources[item.MessageIndex-1]
	// Validate event quotations against only the visible user original.
	source.Source.Text = message.Text
	d := Decision{Item: item.Item}
	if oneOf(item.Kind, "task", "idea") {
		d.Action = Capture
		return d, source
	}
	d.Action, d.Confirmation, d.Subject = Remember, "candidate", item.Subject
	d.EventFrom, d.EventTo, d.EventPrecision = Event(item.When, source, location)
	d.Mentions = append(d.Mentions, GroundedMentions(item.People, "person", grounding)...)
	d.Mentions = append(d.Mentions, GroundedMentions(item.Places, "place", grounding)...)
	d.Mentions = append(d.Mentions, GroundedMentions(item.Organizations, "organization", grounding)...)
	if SelfSubject(item.Subject) {
		d.SubjectType = "self"
	} else if names := GroundedMentions([]string{item.Subject}, "person", grounding); len(names) > 0 {
		d.Subject, d.SubjectType = names[0].Name, "person"
		for _, mention := range d.Mentions {
			if mention.Role == "organization" && strings.EqualFold(mention.Name, d.Subject) {
				d.SubjectType = "organization"
			}
		}
	}
	return d, source
}

// Ordinary messages stay intact. Oversized user messages are supplied in full
// through consecutive bounded fragments, with their original index and time.
// AI proposals remain intact, including in the following user's context.
// SplitConversation returns the segments of a conversation. Each segment has
// the two messages before it as context.
func SplitConversation(sources []memory.SourceResult) []ConversationSegment {
	segments := []ConversationSegment{}
	segment := ConversationSegment{Messages: []ConversationMessage{}, Context: []ConversationMessage{}}
	length := 0
	flush := func() {
		if len(segment.Messages) == 0 {
			return
		}
		segments = append(segments, segment)
		previous := append(append([]ConversationMessage{}, segment.Context...), segment.Messages...)
		segment = ConversationSegment{Messages: []ConversationMessage{}, Context: conversationOverlap(previous)}
		length = 0
	}
	for i, source := range sources {
		text := source.Source.Text
		count := utf8.RuneCountInString(text)
		message := ConversationMessage{Index: i + 1, Role: source.Context.Role, Text: text, ExpressedAt: ExpressedAt(source)}
		if source.Context.Role == "user" && count > segmentCharacters {
			flush()
			runes := []rune(text)
			parts := (count + segmentCharacters - 1) / segmentCharacters
			for start, part := 0, 1; start < count; start, part = start+segmentCharacters, part+1 {
				fragment := message
				fragment.Text = string(runes[start:min(start+segmentCharacters, count)])
				fragment.Part, fragment.Parts = part, parts
				segment.Messages = append(segment.Messages, fragment)
				flush()
			}
			continue
		}
		if length+count > segmentCharacters {
			flush()
		}
		segment.Messages = append(segment.Messages, message)
		length += count
	}
	flush()
	return segments
}

func conversationOverlap(messages []ConversationMessage) []ConversationMessage {
	out := []ConversationMessage{}
	seen := map[int]bool{}
	for i := len(messages) - 1; i >= 0 && len(out) < 2; i-- {
		if !seen[messages[i].Index] {
			seen[messages[i].Index] = true
			out = append([]ConversationMessage{messages[i]}, out...)
		}
	}
	return out
}

// ConversationRun identifies one state of a conversation. A change of a
// message, its order, its branch, or its time gives a new run.
func ConversationRun(sources []memory.SourceResult) string {
	// Source identity/version, branch, role, order and time fence the whole input.
	// Including text also protects redactions that preserve source version.
	type input struct {
		Ref     memory.Ref
		Context *memory.SourceContext
		At      *time.Time
	}
	hash := sha256.New()
	io.WriteString(hash, "conversation-extraction-v"+strconv.Itoa(ConversationVersion)+"\n")
	for _, source := range sources {
		hash.Write(fenced(input{source.Source.Ref, source.Context, ExpressedAt(source)}))
		// Stream originals into the digest without another whole-conversation
		// JSON copy. Length framing keeps record boundaries unambiguous.
		io.WriteString(hash, strconv.Itoa(len(source.Source.Text))+":")
		io.WriteString(hash, source.Source.Text)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// ConversationStage is the stage of the job for one segment of a run. nextRef
// is the number that the next statement of the run gets.
func ConversationStage(run string, segment, nextRef int) string {
	return ConversationPrefix + run + ":" + strconv.Itoa(segment) + ":" + strconv.Itoa(nextRef)
}

// ParseConversationStage reads the run, the segment, and the next statement
// number from a stage.
func ParseConversationStage(stage string) (string, int, int, error) {
	parts := strings.Split(strings.TrimPrefix(stage, ConversationPrefix), ":")
	if len(parts) != 3 || len(parts[0]) != 64 {
		return "", 0, 0, memory.ErrInvalid
	}
	segment, err := strconv.Atoi(parts[1])
	next, nextErr := strconv.Atoi(parts[2])
	if err != nil || nextErr != nil || segment < 0 || next < 1 {
		return "", 0, 0, memory.ErrInvalid
	}
	return parts[0], segment, next, nil
}
