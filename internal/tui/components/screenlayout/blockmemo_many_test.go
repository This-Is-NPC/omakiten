package screenlayout

import (
	"testing"
)

func TestBlockManyBuildsIndependentKeysOnce(t *testing.T) {
	var builds int
	memo := BlockMemo[string]{}
	build := func() Block {
		builds++
		return Block{Items: []string{"card"}}
	}

	memo.BlockMany("left", []string{"left"}, build)
	memo.BlockMany("right", []string{"right"}, build)
	memo.BlockMany("left", []string{"left"}, build)
	memo.BlockMany("right", []string{"right"}, build)

	if builds != 2 {
		t.Fatalf("built %d times, want one build per independent key", builds)
	}
}

func TestBlockManyInputChangesInvalidateOnlyThatKey(t *testing.T) {
	builds := map[string]int{}
	memo := BlockMemo[string]{}
	build := func(key string) func() Block {
		return func() Block {
			builds[key]++
			return Block{Items: []string{key}}
		}
	}

	memo.BlockMany("left", []string{"v1"}, build("left"))
	memo.BlockMany("right", []string{"v1"}, build("right"))
	memo.BlockMany("left", []string{"v2"}, build("left"))
	memo.BlockMany("right", []string{"v1"}, build("right"))

	if builds["left"] != 2 || builds["right"] != 1 {
		t.Fatalf("build counts = %#v, want left=2 right=1", builds)
	}
}

func TestBlockManyCopiesInputWitnesses(t *testing.T) {
	var builds int
	memo := BlockMemo[string]{}
	inputs := []string{"stable"}
	memo.BlockMany("card", inputs, func() Block {
		builds++
		return Block{Items: []string{"card"}}
	})
	inputs[0] = "caller mutation"

	memo.BlockMany("card", []string{"stable"}, func() Block {
		builds++
		return Block{Items: []string{"card"}}
	})
	if builds != 1 {
		t.Fatalf("caller input mutation invalidated a matching entry; built %d times", builds)
	}
}

func TestBlockManyResetDropsEntries(t *testing.T) {
	var builds int
	memo := BlockMemo[string]{}
	build := func() Block {
		builds++
		return Block{Items: []string{"card"}}
	}

	memo.BlockMany("card", []string{"v1"}, build)
	memo.Reset()
	memo.BlockMany("card", []string{"v1"}, build)

	if builds != 2 {
		t.Fatalf("built %d times after Reset, want 2", builds)
	}
}
