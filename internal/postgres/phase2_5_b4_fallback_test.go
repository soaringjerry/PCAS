package postgres_test

import (
	"net/http"
	"testing"
	"time"
)

// R4-5 falls back on memory selection. R4-10 still governs the round's tier.
func TestPhase25B4_MediumPromotionWithoutCards(t *testing.T) {
	for _, mode := range []string{"keyword", "missing", "forced", "heavy"} {
		t.Run(mode, func(t *testing.T) {
			f := phase25B234NewFixture(t)
			model := f.model(t, func(_ *http.Request, n int, _ phase25B234ModelRequest) phase25B234ModelReply {
				if n == 1 {
					time.Sleep(50 * time.Millisecond)
					return phase25B4ReplyJSON(phase25B4Reply{text: "虚构无卡初稿。", missingKeyInformation: mode == "missing"}, false)
				}
				return phase25B4ReplyJSON(phase25B4Reply{text: "虚构无卡自查回复。"}, true)
			})
			question, tier := "处理虚构事项", ""
			if mode == "keyword" {
				question = "仔细处理虚构事项"
			}
			if mode == "forced" {
				tier = "medium"
			}
			if mode == "heavy" {
				tier = "heavy"
			}
			turn := f.secretaryTurn(t, question, tier)
			if len(model.calls()) != 2 {
				t.Errorf("medium without cards calls=%d want 2", len(model.calls()))
			}
			if turn.Turn.Reply != "虚构无卡自查回复。" {
				t.Errorf("reply=%s", turn.Turn.Reply)
			}
			f.tierUsage(t, "medium", map[string]int{"secretary": 1, "selfcheck": 1})
		})
	}
}
