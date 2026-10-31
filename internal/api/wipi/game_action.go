package wipi

// The game actions WIPI's Display answers with, each paired with the key code
// it stands for. The five pad actions are the numbers every runtime of this
// era uses; the soft keys, the volume pair, the side select and clear sit in a
// block of the platform's own above them. KTF's Display and LGT's answer from
// this one table; an SKT Jlet keeps the vendor's own key codes and its own
// table (docs/skvm.md).
//
// **One table, read in both directions.** `getGameAction` and `getKeyCode` are
// inverses, and on KTF they used to be two switches — the forward one knowing
// six pairs and the reverse one twelve. A title that asks what a key means and
// then indexes its own table by the answer is handed the key code straight
// back for every pair only the reverse knew, and a key code here is negative.
// Deriving both directions from one list is what stops the two disagreeing
// again; see docs/history/ktf.md, "A game action is a subscript, and only one
// direction of the table knew it", on what a title did with the answer.
var gameActions = []struct{ action, key int32 }{
	{1, -1},   // up
	{2, -3},   // left
	{5, -4},   // right
	{6, -2},   // down
	{8, -5},   // fire
	{90, -6},  // left soft key
	{91, -7},  // right soft key
	{92, -8},  // third soft key
	{96, -13}, // volume up
	{97, -14}, // volume down
	// Which key this pair names is not something this project has identified.
	// It is what KTF's original reverse table answered for 98, and LGT's table
	// carries it too; it is kept so that the two directions stay inverses.
	{98, -15},
	{99, -16}, // clear
}

// GameAction is Display.getGameAction(keyCode). A key with no action reports
// itself, which is what leaves the digits, star and hash reaching a title's
// own branches as the characters they are.
func GameAction(key int32) int32 {
	for _, pair := range gameActions {
		if pair.key == key {
			return pair.action
		}
	}
	return key
}

// GameActionKey is Display.getKeyCode(gameAction), the reverse. An action with
// no key reports zero.
func GameActionKey(action int32) int32 {
	for _, pair := range gameActions {
		if pair.action == action {
			return pair.key
		}
	}
	return 0
}
