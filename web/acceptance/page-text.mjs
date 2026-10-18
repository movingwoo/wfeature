// The page's words for a route that drives it: text.properties, read the way
// the page's tests read it (../text-fixture.mjs), so that a route asks for a
// sentence by its key and a reworded sentence is not a failure. A route runs a
// server built from the same checkout, which serves the same file.
import "../text-fixture.mjs";
import { phrase } from "../text.js";

export { phrase };

// says reports whether text is key's sentence, whatever fills its placeholders.
export const says = (text, key) => {
  const pattern = phrase(key)
    .split(/\{[A-Za-z][A-Za-z0-9]*\}/)
    .map(part => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
    .join("[\\s\\S]*");
  return new RegExp(`^${pattern}$`).test(text);
};

// rapidFire is what the rapid-fire button says in a mode.
export const rapidFire = mode => phrase("keypad.rapid-fire", { mode: phrase(`keypad.rapid-fire.${mode}`) });
