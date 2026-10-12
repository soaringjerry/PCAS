package jobcall

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestJobWithoutAGatewayIsNotConfigured(t *testing.T) {
	_, _, err := Generator{}.GenerateForJob(context.Background(), worker.Job{}, "organize", prompts.Must("organize"), "", nil, nil)
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != "provider_not_configured" || failure.Retry {
		t.Fatal(err)
	}
}
