package lgt

import "testing"

func TestJavaDisplayGameActionsIncludeSoftAndSideKeys(t *testing.T) {
	pairs := []struct{ key, action int32 }{
		{-1, 1}, {-3, 2}, {-4, 5}, {-2, 6}, {-5, 8},
		{-6, 90}, {-7, 91}, {-8, 92},
		{-13, 96}, {-14, 97}, {-15, 98}, {-16, 99},
	}
	for _, pair := range pairs {
		action, err := javaGameAction(nil, nil, nil, []uint32{uint32(pair.key)})
		if err != nil || int32(action) != pair.action {
			t.Errorf("getGameAction(%d) = %d, %v; want %d", pair.key, int32(action), err, pair.action)
		}
		key, err := javaKeyCode(nil, nil, nil, []uint32{uint32(pair.action)})
		if err != nil || int32(key) != pair.key {
			t.Errorf("getKeyCode(%d) = %d, %v; want %d", pair.action, int32(key), err, pair.key)
		}
	}
}

func TestJavaDisplayPreservesKeysWithoutGameActions(t *testing.T) {
	for _, key := range []int32{'0', '5', '9', '*', '#', -10, -11, -123} {
		action, err := javaGameAction(nil, nil, nil, []uint32{uint32(key)})
		if err != nil || int32(action) != key {
			t.Errorf("getGameAction(%d) = %d, %v; want the original key", key, int32(action), err)
		}
	}
	key, err := javaKeyCode(nil, nil, nil, []uint32{3})
	if err != nil || key != 0 {
		t.Errorf("getKeyCode(3) = %d, %v; want 0 for an unknown action", key, err)
	}
}
