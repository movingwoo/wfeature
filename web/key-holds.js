// A finger dragged across the keypad presses each key it crosses, the way a
// thumb rolls 2-6-8-4 around the direction pad. That turns one touch into a run
// of presses and releases, and two things about the run are easy to get wrong:
// a slide has to release the key it leaves before it presses the one it
// reaches, since the game reads them in the order they arrive, and two fingers
// on the same key — one sliding onto what the other already holds, or onto the
// second button a key is printed on — are owed exactly one press between them.
//
// Both are bookkeeping rather than DOM work, so they live here where a test can
// reach them. app.js supplies press and release; what is under the finger is
// its question to answer.
export const createKeyHolds = ({ press, release }) => {
  // What each pointer holds, or null while it sits somewhere that is not a key.
  // A pointer that has wandered off the pad stays in the map so that sliding
  // back onto a key presses it again.
  const byPointer = new Map();
  // How many pointers hold each key.
  const holders = new Map();
  // Pointers that hold a key but are not followed any further; see latch.
  const latched = new Set();

  const moveTo = (pointerId, name) => {
    const held = byPointer.get(pointerId) ?? null;
    if (held === name) return;
    if (held !== null) {
      const rest = (holders.get(held) ?? 1) - 1;
      if (rest > 0) holders.set(held, rest);
      else {
        holders.delete(held);
        release(held);
      }
    }
    byPointer.set(pointerId, name);
    if (name === null) return;
    const taken = holders.get(name) ?? 0;
    holders.set(name, taken + 1);
    if (taken === 0) press(name);
  };

  return {
    // tracking answers whether this pointer is one the keypad is following. A
    // finger that went down where a slide may not begin is not, and the moves
    // it sends are nobody's business here.
    tracking: pointerId => byPointer.has(pointerId) && !latched.has(pointerId),
    // track follows a pointer that is holding nothing yet — one that went down
    // beside the keys rather than on one. Sliding onto a key presses it, the
    // way a thumb finds a button on a handset without looking.
    track: pointerId => {
      if (!byPointer.has(pointerId)) byPointer.set(pointerId, null);
    },
    moveTo,
    // latch holds a key and stops following the pointer. The keypad's top row
    // works this way: those keys are aimed at one at a time, and a finger that
    // wanders off one of them is not looking for the next one.
    latch: (pointerId, name) => {
      moveTo(pointerId, name);
      latched.add(pointerId);
    },
    lift: pointerId => {
      if (!byPointer.has(pointerId)) return;
      latched.delete(pointerId);
      moveTo(pointerId, null);
      byPointer.delete(pointerId);
    },
  };
};
