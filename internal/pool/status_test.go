package pool

import (
	"testing"
	"time"

	"github.com/kunchenguid/treehouse/v3/internal/process"
)

// listDuringProcessScan runs List and calls during while the listing is held
// in its unlocked process scan, after the state snapshot was taken.
func listDuringProcessScan(t *testing.T, poolDir string, during func()) []WorktreeStatus {
	t.Helper()
	scanning, resume := make(chan struct{}), make(chan struct{})
	restore := swapListProcessSeams(
		func(string) ([]process.ProcessInfo, error) {
			select {
			case <-scanning:
			default:
				close(scanning)
			}
			<-resume
			return nil, nil
		},
		func(procs []process.ProcessInfo) ([]process.ProcessInfo, error) { return procs, nil },
	)
	t.Cleanup(restore)
	listed := make(chan struct{})
	var statuses []WorktreeStatus
	var listErr error
	go func() {
		statuses, listErr = List(poolDir)
		close(listed)
	}()
	t.Cleanup(func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
		<-listed
	})
	select {
	case <-scanning:
	case <-time.After(10 * time.Second):
		t.Fatal("listing never reaches process inspection")
	}
	during()
	close(resume)
	<-listed
	if listErr != nil {
		t.Fatal(listErr)
	}
	return statuses
}

// A slow observation must not prevent an independent caller from protecting
// its worktree with a durable lease. The listing must not overwrite that lease,
// and must report it rather than the stale snapshot.
func TestListAllowsLeaseDuringProcessInspection(t *testing.T) {
	repo, poolDir := setupRepo(t)
	path, err := Acquire(repo, poolDir, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	clearOwnerReservation(t, poolDir, path)
	statuses := listDuringProcessScan(t, poolDir, func() {
		leased := make(chan struct{})
		var leaseErr error
		go func() {
			_, leaseErr = LeaseExisting(poolDir, "1", "during-status")
			close(leased)
		}()
		select {
		case <-leased:
			if leaseErr != nil {
				t.Fatal(leaseErr)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a slow status observation blocks lease acquisition")
		}
	})
	if entry := entryFor(t, poolDir, path); !entry.Leased || entry.LeaseHolder != "during-status" {
		t.Fatalf("listing loses the concurrent lease: %+v", entry)
	}
	if len(statuses) != 1 || statuses[0].Status != StatusLeased || statuses[0].LeaseHolder != "during-status" {
		t.Fatalf("listing reports the pre-lease snapshot instead of the concurrent lease: %+v", statuses)
	}
}

// A slot removed after the listing's snapshot is no longer in the pool, so it
// must be omitted rather than reported damaged for its missing marker.
func TestListOmitsSlotDestroyedDuringProcessInspection(t *testing.T) {
	repo, poolDir := setupRepo(t)
	kept, err := Acquire(repo, poolDir, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := Acquire(repo, poolDir, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	clearOwnerReservation(t, poolDir, kept)
	clearOwnerReservation(t, poolDir, removed)
	statuses := listDuringProcessScan(t, poolDir, func() {
		result, err := DestroyWorktree(poolDir, removed, DestroyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Destroyed) != 1 {
			t.Fatalf("destroy during listing removed nothing: %+v", result)
		}
	})
	if len(statuses) != 1 || statuses[0].Path != kept || statuses[0].Status != StatusAvailable {
		t.Fatalf("listing reports a slot destroyed after its snapshot: %+v", statuses)
	}
}
