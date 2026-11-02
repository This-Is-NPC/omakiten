package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestReadWorkRecordTerminatesOnParentCycle(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	uids := seedRowGraph(t, store, project.ID)
	mustExec(t, store, `UPDATE tasks SET parent_id = (SELECT id FROM tasks WHERE uid = ?) WHERE uid = ?`, uids["child"], uids["parent"])
	taskID := mustScan[int64](t, store, `SELECT id FROM tasks WHERE uid = ?`, uids["parent"])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	record, err := store.ReadWorkRecord(ctx, project.ID, "", taskID, newStoreFixture(t, store).snap())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Tasks) != 2 {
		t.Fatalf("tasks = %d, want the 2 members of the cycle", len(record.Tasks))
	}
}
