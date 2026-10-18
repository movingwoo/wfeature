// The page's words. Every sentence and label a player reads is written in
// text.properties, so that the wording can be changed in one file without
// reading the code that shows it. The server fills the markup's own text from
// that file before the page arrives, and puts the whole table in the page as
// the JSON in #page-text; this module is how a script reads it.
//
// A script asks for a sentence when it shows one, never while it is being
// loaded: app.js installs the table, and every module it imports has already
// run by then.

let table = Object.create(null);

// useText installs a table: the page's own at start-up, a test's in a test.
export const useText = entries => {
  table = Object.assign(Object.create(null), entries);
};

// readPageText answers the table the server put in the page. A page without
// one answers an empty table, and every sentence then shows as its key, which
// names what is missing instead of showing nothing.
export const readPageText = document => {
  try {
    const parsed = JSON.parse(document?.getElementById?.("page-text")?.textContent ?? "");
    return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return {};
  }
};

const placeholder = /\{([A-Za-z][A-Za-z0-9]*)\}/g;

// phrase answers the sentence a key names, with each {name} in it replaced by
// values[name]. A name the values do not have stays as it is written, and a key
// the table does not have answers itself.
export const phrase = (key, values) => {
  const sentence = table[key] ?? key;
  if (!values) return sentence;
  return sentence.replace(placeholder, (whole, name) => (Object.hasOwn(values, name) ? String(values[name]) : whole));
};
