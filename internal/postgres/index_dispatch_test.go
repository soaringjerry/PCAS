package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestIndexDispatchRunsAlongsideConversationAndHonorsPause(t *testing.T) {
	s, scope := testStore(t), owner()
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "assistant"}, []string{"合成原文。", "合成上下文。"}))
	b4bOperation(t, s, scope, a.Batch, "organize")
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, a.Sources[0], "source.extract")); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"source.tokenize", "source.embed"} {
		b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage) VALUES(gen_random_uuid(),$1,$2,1,$3)`, string(scope.OwnerID), string(a.Sources[0].ID), stage)
	}
	conversation, err := s.Claim(context.Background(), time.Minute)
	if err != nil || conversation == nil || !strings.HasPrefix(conversation.Stage, conversationExtractionPrefix) {
		t.Fatal("expected leased conversation", conversation, err)
	}
	job, err := s.ClaimIndex(context.Background(), time.Minute)
	if err != nil || job == nil || job.Stage != "source.tokenize" {
		t.Fatal("conversation hid index task", job, err)
	}
	b2Exec(t, s, `UPDATE import_batches SET state='paused' WHERE id=$1`, string(a.Batch))
	if j, err := s.ClaimIndex(context.Background(), time.Minute); err != nil || j != nil {
		t.Fatal("paused import index was claimed", j, err)
	}
	b2Exec(t, s, `UPDATE import_batches SET state='done' WHERE id=$1`, string(a.Batch))
	job, err = s.ClaimIndex(context.Background(), time.Minute)
	if err != nil || job == nil || job.Stage != "source.embed" {
		t.Fatal("resumed vector task not claimed", job, err)
	}
}
