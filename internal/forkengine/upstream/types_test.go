package upstream

import "testing"

func TestBlockRefNormalizeDefaultsToLatest(t *testing.T) {
	ref := (BlockRef{}).Normalize()
	if ref.Tag != BlockTagLatest {
		t.Fatalf("normalized tag = %q, want %q", ref.Tag, BlockTagLatest)
	}
	if ref.Number != nil {
		t.Fatalf("normalized number = %v, want nil", ref.Number)
	}
}

func TestBlockRefCacheKeySeparatesPinnedAndTag(t *testing.T) {
	latest := LatestBlock().CacheKey()
	pinned := BlockNumber(17).CacheKey()
	if latest == pinned {
		t.Fatalf("cache keys should differ: latest=%q pinned=%q", latest, pinned)
	}
	if !BlockNumber(17).IsPinned() {
		t.Fatalf("BlockNumber(17).IsPinned() = false, want true")
	}
}
