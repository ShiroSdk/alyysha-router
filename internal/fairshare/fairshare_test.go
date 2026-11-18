// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package fairshare

import (
	"strconv"
	"testing"
	"time"
)

// TestShareSumsWindow proves Add credits and Share sums, and that an unknown
// key shares zero — the "served the least" weight a fresh key carries.
func TestShareSumsWindow(t *testing.T) {
	Reset()
	Share("k1") // a read alone must not create a ledger entry with weight
	if Add("k1", 0); Share("k1") != 0 {
		t.Errorf("zero-token add changed the share")
	}
	Add("k1", 100)
	Add("k1", 250)
	if got := Share("k1"); got != 350 {
		t.Errorf("share = %v, want 350", got)
	}
	if got := Share("unknown"); got != 0 {
		t.Errorf("unknown key share = %v, want 0", got)
	}
	// The empty key is unattributable: it must never carry weight.
	Add("", 1000)
	if got := Share(""); got != 0 {
		t.Errorf("empty key share = %v, want 0", got)
	}
}

// TestBucketAdvance proves the window rolls: a key silent for a whole bucket
// span forgets the bucket it was served in.
func TestBucketAdvance(t *testing.T) {
	Reset()
	base := time.Unix(1_800_000_000, 0)
	now := base
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })

	Add("roll", 500)
	if got := Share("roll"); got != 500 {
		t.Fatalf("share before roll = %v, want 500", got)
	}
	now = base.Add(bucketSpan + time.Minute)
	if got := Share("roll"); got != 0 {
		t.Errorf("share after a full span = %v, want 0", got)
	}
	Add("roll", 200)
	if got := Share("roll"); got != 200 {
		t.Errorf("share after re-add = %v, want 200", got)
	}
}

// TestEvictionCap proves the ledger drops stale entries past the cap rather
// than growing without bound, and keeps entries the window still covers.
func TestEvictionCap(t *testing.T) {
	Reset()
	base := time.Unix(1_800_000_000, 0)
	now := base
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })

	Add("stale", 10)
	// Fill the map past the cap from the future: the stale sweep the cap
	// triggers drops every entry the window no longer covers.
	now = base.Add(2 * bucketSpan)
	for i := 0; i < maxKeys+16; i++ {
		Add("key-"+strconv.Itoa(i), 1)
	}
	if got := Share("stale"); got != 0 {
		t.Errorf("stale key survived eviction, share = %v", got)
	}
	// A key the filler touched is still inside the window and keeps its
	// share.
	last := "key-" + strconv.Itoa(maxKeys+15)
	if got := Share(last); got != 1 {
		t.Errorf("live key share = %v, want 1", got)
	}
}
