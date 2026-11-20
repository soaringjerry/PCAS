package secretary

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// A turn shows at most three links of at most 500 bytes each, on the web.
const (
	cardLinks     = 3
	cardLinkBytes = 500
)

// attachmentRunes is the text of one attachment that a turn reads.
const attachmentRunes = 12000

// TimelineMetadata is what the owner's cards show of a cited memory: when it
// was said, where it came from, the items its turn made, and whether it was
// changed.
type TimelineMetadata struct {
	Memory  workspace.Memory
	Source  TimelineSource
	Tasks   []TimelineTask
	Changed bool
}

// CardUnit reads for the cards of one turn.
type CardUnit interface {
	// Timeline reads the metadata of the cited memories by their IDs.
	Timeline(ctx context.Context, refs []memory.Ref) (map[string]TimelineMetadata, error)
	Item(ctx context.Context, id string) (workspace.Item, error)
}

// Cards are what the owner sees under a reply: the sources the answer used,
// its links, the timeline of the cited memories when it helps, and the
// tasks it showed with their current due date and status. Provenance is
// resolved for the owner only; it is never added to the model input.
func Cards(ctx context.Context, u CardUnit, answer Output, sent map[string]workspace.Memory, sentSources map[string]workspace.DeskSourceItem, aliases map[string]workspace.Item, recall bool) ([]workspace.DeskCard, error) {
	cards := []workspace.DeskCard{}
	sources := []workspace.DeskSourceItem{}
	timeline := []workspace.DeskTimelineItem{}
	refs := []memory.Ref{}
	for _, alias := range answer.Used {
		if _, ok := sentSources[alias]; ok {
			continue
		}
		if m, ok := sent[alias]; ok {
			refs = append(refs, memory.Ref{ID: memory.ID(m.ID), Version: m.Version})
		}
	}
	metadata, err := u.Timeline(ctx, uniqueRefs(refs))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, alias := range answer.Used {
		if source, ok := sentSources[alias]; ok {
			if !seen[source.MemoryID] {
				seen[source.MemoryID] = true
				sources = append(sources, source)
			}
			// Source citations do not take part in this batch's timeline.
			continue
		}
		m, ok := sent[alias]
		if !ok || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		// Claim grants do not necessarily grant the model the source itself.
		meta := metadata[m.ID]
		if len(m.Sources) == 0 && meta.Source.SourceID != "" {
			m.Sources = []workspace.SourceRef{meta.Source.SourceRef}
		}
		source := workspace.DeskSourceItem{Kind: "claim", MemoryID: m.ID, Version: m.Version, Text: m.Text}
		if len(m.Sources) > 0 {
			source.SourceID = m.Sources[0].SourceID
			source.SourceVersion = m.Sources[0].Version
			source.At = optional(m.Sources[0].At)
		}
		sources = append(sources, source)
		entry := workspace.DeskTimelineItem{
			At: TimelineAt(meta.Memory.ExpressedAt, meta.Source), Text: m.Text,
			Status: TimelineStatus(meta.Tasks, meta.Changed), MemoryID: optional(m.ID),
			EventFrom: meta.Memory.EventFrom, EventTo: meta.Memory.EventTo,
			EventPrecision: meta.Memory.EventPrecision, Mentions: meta.Memory.Mentions,
		}
		if len(meta.Tasks) > 0 {
			entry.ThingID = optional(meta.Tasks[0].ID)
		}
		timeline = append(timeline, entry)
	}
	if len(sources) > 0 {
		cards = append(cards, workspace.DeskCard{Kind: "sources", Items: sources})
	}
	if links := Links(answer.Links); len(links) > 0 {
		cards = append(cards, workspace.DeskCard{Kind: "links", Items: links})
	}
	if TimelineVisible(timeline, recall) {
		sort.SliceStable(timeline, func(i, j int) bool { return TimelineBefore(timeline[i], timeline[j]) })
		cards = append(cards, workspace.DeskCard{Kind: "timeline", Title: "相关记录", Items: timeline})
	}
	tasks := []workspace.DeskTaskItem{}
	seen = map[string]bool{}
	for _, alias := range answer.Show {
		item, ok := aliases[alias]
		if !ok || item.Kind != "task" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		current, err := u.Item(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		// The title was sanitized before generation; use the fresh due/status only.
		task := workspace.DeskTaskItem{ThingID: item.ID, Title: item.Title, Due: optional(current.Due), Status: current.Status}
		if current.ProjectID != "" {
			for _, p := range aliases {
				if p.ID == current.ProjectID {
					task.Project = optional(p.Title)
					break
				}
			}
		}
		tasks = append(tasks, task)
	}
	if len(tasks) > 0 {
		cards = append(cards, workspace.DeskCard{Kind: "tasks", Items: tasks})
	}
	return cards, nil
}

// Links are the web links of an answer that the owner may open: http or
// https with a host and no user, at most three.
func Links(raw []string) []workspace.DeskLinkItem {
	links := []workspace.DeskLinkItem{}
	for _, link := range raw {
		u, err := url.Parse(link)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && len(link) <= cardLinkBytes && len(links) < cardLinks {
			links = append(links, workspace.DeskLinkItem{URL: u.String(), Host: u.Hostname()})
		}
	}
	return links
}

// AttachmentInput is what a turn reads of an attachment, or the warning the
// owner gets when it could not be read. An image needs a reading by vision.
func AttachmentInput(title, media, representation, text string, readErr error) (input, warning string) {
	image := strings.HasPrefix(media, "image/")
	if readErr != nil || image && representation != "vision" {
		if image {
			return "", "图片存好了，但我现在看不了图；请检查模型通道，或把要办的内容写成文字。"
		}
		return "", "文件存好了，但暂时没读出内容；请检查资料处理状态，或把要办的内容写成文字。"
	}
	if r := []rune(text); len(r) > attachmentRunes {
		text = string(r[:attachmentRunes])
	}
	return fmt.Sprintf("附件 %s（%s）：\n%s", title, representation, text), ""
}

// AttachmentReceipt is the receipt of a stored attachment.
func AttachmentReceipt(media string) string {
	switch {
	case strings.HasPrefix(media, "image/"):
		return "存了一张图片"
	case strings.HasPrefix(media, "audio/"):
		return "存了一段音频"
	}
	return "存了一份文件"
}

func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
