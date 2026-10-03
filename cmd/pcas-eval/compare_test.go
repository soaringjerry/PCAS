package main

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/fixture"
)

func TestBoundedContextPreservesUTF8AndCountsDroppedDocuments(t *testing.T) {
	docs := []fixture.Document{{Title: "旧话", Text: strings.Repeat("竹", 20)}, {Title: "新话", Text: "second"}}
	context, truncated, full, dropped := boundedContext(docs, time.Now(), 63)
	if !utf8.ValidString(context) || len(context) > 63 || !truncated || full != 0 || dropped != 2 {
		t.Fatal(len(context), truncated, full, dropped)
	}
}
func TestComparisonKeepsQuestionAndExcludesOriginalRetrievedText(t *testing.T) {
	prompt := "history\n召回的记忆\noriginal gold\n相关原话\nold source\nTHIS：\n这句话：question"
	result, err := replaceContext(prompt, "replacement source")
	if err != nil || strings.Contains(result, "original gold") || strings.Contains(result, "old source") || !strings.Contains(result, "history") || !strings.Contains(result, "question") {
		t.Fatal(result, err)
	}
}
func TestTemporaryDatabaseRejectsRemoteBeforeConnection(t *testing.T) {
	_, _, _, err := openTemporary(context.Background(), "postgres://unused:unused@192.0.2.1/pcas_eval")
	if err == nil || !strings.Contains(err.Error(), "eval_database_not_local") {
		t.Fatal(err)
	}
}
func TestVectorDimensionMismatchAndCosineRanking(t *testing.T) {
	if _, err := cosine([]float32{1}, []float32{1, 2}); err == nil {
		t.Fatal("accepted mixed spaces")
	}
	value, err := cosine([]float32{0, 1}, []float32{1, 0})
	if err != nil || value != 0 {
		t.Fatal(value, err)
	}
}

func TestTemporaryDatabaseRejectsRemoteFallback(t *testing.T) {
	_, _, _, err := openTemporary(context.Background(), "host=127.0.0.1,192.0.2.1 user=unused dbname=pcas_eval")
	if err == nil || !strings.Contains(err.Error(), "eval_database_not_local") {
		t.Fatal(err)
	}
}
