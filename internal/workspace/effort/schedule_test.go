package effort

import (
	"reflect"
	"testing"
)

func TestDueQueuesProjectsWithUnestimatedTasksAndNoPendingJob(t *testing.T) {
	projects := []Project{
		{ID: "p1", NeedsEstimate: true, Pending: false},
		{ID: "p2", NeedsEstimate: true, Pending: true},
		{ID: "p3", NeedsEstimate: false, Pending: false},
		{ID: "p4", NeedsEstimate: true, Pending: false},
	}
	got := Due(projects)
	want := []string{"p1", "p4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
