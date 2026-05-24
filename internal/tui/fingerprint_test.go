package tui

import (
	"encoding/binary"
	"hash/fnv"
	"testing"
)

// TestFingerprintInt64MatchesLittleEndianFnv64a pins the wire shape
// callers depend on: writeInt64 must emit eight bytes in LittleEndian
// order through fnv64a. Production code that worked before the helper
// extraction wrote `binary.LittleEndian.PutUint64(buf[:], uint64(v))`
// then `h.Write(buf[:])`; the helper has to produce the same digest so
// post-migration cache hits still match the pre-migration values when
// computed against equivalent inputs.
func TestFingerprintInt64MatchesLittleEndianFnv64a(t *testing.T) {
	f := newFingerprint()
	f.writeInt64(42)
	got := f.sum()

	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(42))
	_, _ = h.Write(buf[:])
	want := h.Sum64()

	if got != want {
		t.Fatalf("fingerprint.writeInt64 digest = %x, want %x (LittleEndian fnv64a)", got, want)
	}
}

// TestFingerprintStringEmitsNullSeparator pins the writeString contract:
// the helper must append a 0-byte after the string so "ab"+"c" and
// "a"+"bc" produce different digests. Without the separator, every
// callsite that mixes adjacent strings would suffer the canonical
// concatenation collision and serve stale cache hits across distinct
// inputs.
func TestFingerprintStringEmitsNullSeparator(t *testing.T) {
	left := newFingerprint()
	left.writeString("ab")
	left.writeString("c")
	leftSum := left.sum()

	right := newFingerprint()
	right.writeString("a")
	right.writeString("bc")
	rightSum := right.sum()

	if leftSum == rightSum {
		t.Fatalf("writeString failed to disambiguate adjacent strings: ab|c digest = %x equals a|bc; null separator missing", leftSum)
	}
}

// TestFingerprintInt64SliceIsOrderInsensitive pins the writeInt64Slice
// contract: the helper sorts its input before writing so callers that
// pass tag ids from a DB row scan (whose order tracks insertion, not
// id) get a stable fingerprint even when the same ids come back in a
// different order on a subsequent render.
func TestFingerprintInt64SliceIsOrderInsensitive(t *testing.T) {
	ascending := newFingerprint()
	ascending.writeInt64Slice([]int64{1, 2, 3})
	ascSum := ascending.sum()

	reversed := newFingerprint()
	reversed.writeInt64Slice([]int64{3, 2, 1})
	revSum := reversed.sum()

	if ascSum != revSum {
		t.Fatalf("writeInt64Slice order-sensitive: asc digest = %x, reversed = %x; sort missing", ascSum, revSum)
	}

	// And a different set must still diverge — the sort must not
	// collapse the input to a no-op.
	other := newFingerprint()
	other.writeInt64Slice([]int64{4, 5, 6})
	otherSum := other.sum()
	if ascSum == otherSum {
		t.Fatalf("writeInt64Slice collapsed two different sets to same digest: %x", ascSum)
	}
}

// TestFingerprintBoolDistinguishesTrueFalse pins the writeBool
// contract: true and false must produce different digests so the plan-
// network cache key can partition on a wave's collapsed state.
func TestFingerprintBoolDistinguishesTrueFalse(t *testing.T) {
	tr := newFingerprint()
	tr.writeBool(true)
	trSum := tr.sum()

	fa := newFingerprint()
	fa.writeBool(false)
	faSum := fa.sum()

	if trSum == faSum {
		t.Fatalf("writeBool produced identical digest for true / false: %x", trSum)
	}
}

// TestFingerprintSliceDoesNotMutateCaller pins the defensive-copy
// contract: writeInt64Slice sorts a local copy so the caller's
// backing array is never reordered. Without the copy, a render-cache
// fingerprint of `[]int64{tag.ID for tag in row.Tags}` would silently
// reorder the underlying slice and break any later iteration that
// expected insertion order.
func TestFingerprintSliceDoesNotMutateCaller(t *testing.T) {
	source := []int64{3, 1, 2}
	original := append([]int64(nil), source...)

	f := newFingerprint()
	f.writeInt64Slice(source)
	_ = f.sum()

	for i, v := range original {
		if source[i] != v {
			t.Fatalf("writeInt64Slice mutated caller slice at index %d: want %d, got %d", i, v, source[i])
		}
	}
}
