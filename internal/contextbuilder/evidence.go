package contextbuilder

import (
	"context"
	"errors"
	"slices"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// The source context of the memories that one input gives: at most this many
// conversations, each message cut to this many characters around the quote,
// and this many characters in all.
const (
	evidenceGroups       = 4
	evidenceMessageRunes = 900
	evidenceLeadRunes    = 200
	evidenceRunes        = 10000
)

// What a conversation window cannot show, in the words that the model and
// the owner read. Storage reads the window and adds the sentence that fits.
const (
	WindowEarlierVersion = "引用原话是较早版本；周边消息按目前保留的版本显示。"
	WindowNoConversation = "这份原文没有可展开的会话归属。"
	WindowHistorical     = "这条属于历史旁支，仅显示引用原话，避免混合不同分支。"
	WindowPartial        = "部分上下文不可用，仅能核对引用原话。对象和原因不明时不要补猜。"
)

// clippedMessage ends a message that is cut to its part around the quote.
const clippedMessage = "\n（此消息仅显示片段，未读部分可能包含限定。）"

// EvidenceAnchor is where the evidence of a memory sits in its source: the
// source version and the character range of the quote.
type EvidenceAnchor struct {
	Source     memory.Ref
	Start, End int
}

// EvidenceSource reads the evidence of memories for one reader.
type EvidenceSource interface {
	// Anchor is the evidence of the memory in a current conversation; ok is
	// false when it has none.
	Anchor(ctx context.Context, ref memory.Ref) (anchor EvidenceAnchor, ok bool, err error)
	// Window is the part of the conversation around the source that the
	// reader may see. memory.ErrNotFound or memory.ErrForbidden means none.
	Window(ctx context.Context, source memory.Ref) (memory.ConversationResult, error)
}

// Evidence gives the source conversations of the claims within the budget,
// and a sentence for each claim whose context is left out. A claim whose
// meaning depends on its context goes first. Two claims of one source share
// its window unless the quote of the first was cut.
func Evidence(ctx context.Context, src EvidenceSource, claims []Claim) ([]EvidenceGroup, []string, error) {
	groups := []EvidenceGroup{}
	gaps := []string{}
	claims = slices.Clone(claims)
	slices.SortStableFunc(claims, func(a, b Claim) int {
		if memory.ContextDependent(a.Text) == memory.ContextDependent(b.Text) {
			return 0
		}
		if memory.ContextDependent(a.Text) {
			return -1
		}
		return 1
	})
	characters := 0
	seen := map[memory.Ref]int{}
	clippedQuote := map[int]bool{}
	for _, claim := range claims {
		anchor, ok, err := src.Anchor(ctx, claim.Ref)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			if memory.ContextDependent(claim.Text) {
				gaps = append(gaps, claim.Label+" 含未明确指代，当前没有可用的原对话上下文；不要猜测对象。")
			}
			continue
		}
		if index, ok := seen[anchor.Source]; ok && !clippedQuote[index] {
			groups[index].Label += ", " + claim.Label
			continue
		}
		if len(groups) >= evidenceGroups {
			gaps = append(gaps, claim.Label+" 的来源上下文尚未补齐；涉及对象、原因或限定时请说明资料不足。")
			continue
		}
		window, err := src.Window(ctx, anchor.Source)
		if errors.Is(err, memory.ErrNotFound) || errors.Is(err, memory.ErrForbidden) {
			gaps = append(gaps, claim.Label+" 的证据上下文不可用。不要猜测对象。")
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		clipped := false
		for i := range window.Messages {
			msg := &window.Messages[i]
			text := []rune(msg.Text)
			if len(text) <= evidenceMessageRunes {
				continue
			}
			at := 0
			if msg.Anchor {
				clipped = true
				at = max(0, min(anchor.Start-evidenceLeadRunes, len(text)-evidenceMessageRunes))
				if anchor.End-anchor.Start > evidenceMessageRunes {
					window.Gaps = append(window.Gaps, "引用原话本身超过阅读预算，当前片段无法覆盖全部限定。")
				}
			}
			msg.Text = string(text[at:at+evidenceMessageRunes]) + clippedMessage
		}
		count := 0
		for _, m := range window.Messages {
			count += len([]rune(m.Text))
		}
		if characters+count > evidenceRunes {
			gaps = append(gaps, claim.Label+" 的来源上下文超过本次预算；不要根据摘要补猜。")
			continue
		}
		characters += count
		seen[anchor.Source] = len(groups)
		clippedQuote[len(groups)] = clipped
		groups = append(groups, EvidenceGroup{Label: claim.Label, Window: window})
	}
	return groups, gaps, nil
}
