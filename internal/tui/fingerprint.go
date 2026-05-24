package tui

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"sort"
)

// fingerprint wraps the fnv64a + binary.LittleEndian pattern five
// render-cache keys repeat (activityRowsForRenderKey,
// planNetworkRowsCacheKey, taskDetailsBoxHeightKey, fnv64aString for
// the token cache, and tag-id slice hashing). Each writer encodes its
// value with a stable wire shape — LittleEndian for fixed-width ints,
// null-terminated bytes for strings — so the resulting uint64 stays
// stable across calls with identical inputs (the contract every cache
// callsite relies on for hit/miss correctness).
//
// Why a wrapper instead of inline calls — the inline shape required
// every callsite to declare a local `[8]byte` buffer, remember to
// LittleEndian.PutUint64 + Write, and remember to append a null byte
// after every string. The wrapper removes the foot-guns and makes the
// "wrong order / wrong endianness" bug class one-helper deep.
//
// Not thread-safe: a single fingerprint instance owns its hash.Hash64
// state. Callsites build a fresh fingerprint per key, write its
// inputs, and call sum() — fits the per-render value-receiver path.
type fingerprint struct {
	h   hash.Hash64
	buf [8]byte
}

// newFingerprint returns a fresh fnv64a-backed fingerprint. The fnv64a
// choice mirrors the existing callsites — fast, no allocations after
// the initial state, collision rate is statistically negligible across
// the per-session cache populations TUI renders ever see.
func newFingerprint() *fingerprint {
	return &fingerprint{h: fnv.New64a()}
}

// writeInt64 mixes v into the digest in LittleEndian wire order. The
// signed→unsigned cast is intentional: callers feed task ids, widths,
// counts, and priorities through this method, and a negative value
// (rare — e.g. priority sentinel) hashes the same uint64 as its
// two's-complement bit pattern would on disk. Stability is what the
// callers need; semantic interpretation of the bits is not.
func (f *fingerprint) writeInt64(v int64) {
	binary.LittleEndian.PutUint64(f.buf[:], uint64(v))
	_, _ = f.h.Write(f.buf[:])
}

// writeString mixes s into the digest followed by a single 0-byte
// separator. The separator guards against the canonical "abc" + "de"
// vs "ab" + "cde" collision: without it, the two concatenations
// hash identically. Callers depend on this for any key that mixes a
// variable-length string with adjacent inputs (most do).
func (f *fingerprint) writeString(s string) {
	_, _ = f.h.Write([]byte(s))
	_, _ = f.h.Write([]byte{0})
}

// writeInt64Slice mixes a sorted-ascending copy of vs into the digest.
// The sort makes the helper order-insensitive: callers pass tag ids,
// collapsed-wave keys, or any slice where the source ordering depends
// on DB row order / map iteration and would otherwise ghost-invalidate
// the cache on every render. The slice is copied so the caller's
// backing array is never mutated.
func (f *fingerprint) writeInt64Slice(vs []int64) {
	if len(vs) == 0 {
		return
	}
	sorted := make([]int64, len(vs))
	copy(sorted, vs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, v := range sorted {
		f.writeInt64(v)
	}
}

// writeBool mixes b into the digest as a single byte (0 / 1). Used by
// callsites whose cache key partitions on a yes/no flag (e.g. a wave's
// collapsed state) without the cost of a full 8-byte int write.
func (f *fingerprint) writeBool(b bool) {
	if b {
		_, _ = f.h.Write([]byte{1})
		return
	}
	_, _ = f.h.Write([]byte{0})
}

// sum returns the final uint64 fingerprint. Safe to call once per
// instance; calling sum twice returns the same value but is a smell
// that the caller is reusing the instance across keys (use a fresh
// newFingerprint() per key instead).
func (f *fingerprint) sum() uint64 {
	return f.h.Sum64()
}
