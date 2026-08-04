package cache

import (
	"fmt"
	"strings"
	"testing"
)

func TestBadgerCache_Has(t *testing.T) {
	err := testBadgerCache.Forget("foo")
	if err != nil {
		t.Error(err)
	}

	inCache, err := testBadgerCache.Has("foo")
	if err != nil {
		t.Error(err)
	}

	if inCache {
		t.Error("foo should not be in cache")
	}

	err = testBadgerCache.Set("foo", "bar")
	if err != nil {
		t.Error(err)
	}

	inCache, err = testBadgerCache.Has("foo")
	if err != nil {
		t.Error(err)
	}

	if !inCache {
		t.Error("foo should be in cache")
	}
}

func TestBadgerCache_Get(t *testing.T) {
	err := testBadgerCache.Forget("foo")
	if err != nil {
		t.Error(err)
	}

	_, err = testBadgerCache.Get("foo")
	if err == nil {
		t.Error("foo should not be in cache")
	}

	err = testBadgerCache.Set("foo", "bar")
	if err != nil {
		t.Error(err)
	}

	val, err := testBadgerCache.Get("foo")
	if err != nil {
		t.Error(err)
	}

	if val != "bar" {
		t.Error("foo should be bar")
	}
}

func TestBadgerCache_Set(t *testing.T) {
	err := testBadgerCache.Forget("foo")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.Set("foo", "bar")
	if err != nil {
		t.Error(err)
	}

	val, err := testBadgerCache.Get("foo")
	if err != nil {
		t.Error(err)
	}

	if val != "bar" {
		t.Error("foo should be bar")
	}

	// Test setting with expiration
	err = testBadgerCache.Set("time", "exp", 1)
	if err != nil {
		t.Error(err)
	}

	val, err = testBadgerCache.Get("time")
	if err != nil {
		t.Error(err)
	}

	if val != "exp" {
		t.Error("time should be exp")
	}
}

func TestBadgerCache_Forget(t *testing.T) {
	err := testBadgerCache.Set("foo", "bar")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.Forget("foo")
	if err != nil {
		t.Error(err)
	}

	_, err = testBadgerCache.Get("foo")
	if err == nil {
		t.Error("foo should not be in cache")
	}
}

func TestBadgerCache_Flush(t *testing.T) {
	err := testBadgerCache.Set("foo", "bar")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.Flush()
	if err != nil {
		t.Error(err)
	}

	_, err = testBadgerCache.Get("foo")
	if err == nil {
		t.Error("foo should not be in cache")
	}
}

func TestBadgerCache_EmptyByMatch(t *testing.T) {
	err := testBadgerCache.Forget("foo")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.Set("foo", "bar")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.Set("bar", "baz")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.Set("foo:bar", "baz")
	if err != nil {
		t.Error(err)
	}

	err = testBadgerCache.EmptyByMatch("foo*")
	if err != nil {
		t.Error(err)
	}

	_, err = testBadgerCache.Get("foo")
	if err == nil {
		t.Error("foo should not be in cache")
	}

	_, err = testBadgerCache.Get("foo:bar")
	if err == nil {
		t.Error("foo:bar should not be in cache")
	}

	_, err = testBadgerCache.Get("bar")
	if err != nil {
		t.Error("bar should be in cache")
	}
}

// TestBadgerCache_EmptyByMatchDoesNotAliasIteratorBuffers is the real guard for
// issue #21. badger reuses the buffer behind Item.Key() on the next iteration,
// so collecting Key() rather than KeyCopy(nil) leaves the accumulated slice
// pointing at memory that later keys overwrite -- deleting whatever the buffer
// happened to hold by the time the write transaction ran.
//
// The original test used three short keys and did not reproduce it: too few
// iterations to force reuse, and all the keys short enough to sit in the same
// buffer without visible corruption. It kept passing when badger v4 replaced
// v3, with or without the fix, which made it a guard in name only.
//
// This one interleaves 200 keys across two prefixes with varying lengths, so an
// aliased key read after the loop resolves to a different key than the one that
// matched. Verified to fail with Key() and pass with KeyCopy(nil).
func TestBadgerCache_EmptyByMatchDoesNotAliasIteratorBuffers(t *testing.T) {
	const n = 100

	for i := 0; i < n; i++ {
		// Varying-length suffixes: a shorter key written over a longer one
		// leaves the tail of the previous key visible in the reused buffer.
		pad := strings.Repeat("x", i%17)

		if err := testBadgerCache.Set(fmt.Sprintf("doomed:%d%s", i, pad), "gone"); err != nil {
			t.Fatal(err)
		}
		if err := testBadgerCache.Set(fmt.Sprintf("keeper:%d%s", i, pad), "kept"); err != nil {
			t.Fatal(err)
		}
	}

	if err := testBadgerCache.EmptyByMatch("doomed:*"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < n; i++ {
		pad := strings.Repeat("x", i%17)

		if _, err := testBadgerCache.Get(fmt.Sprintf("doomed:%d%s", i, pad)); err == nil {
			t.Errorf("doomed:%d%s survived EmptyByMatch", i, pad)
		}
		if _, err := testBadgerCache.Get(fmt.Sprintf("keeper:%d%s", i, pad)); err != nil {
			t.Errorf("keeper:%d%s was deleted by EmptyByMatch(\"doomed:*\")", i, pad)
		}
	}
}
