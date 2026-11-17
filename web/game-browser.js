// The game browser: a window that shows the library one folder at a time.
//
// The picker is a select, and a select over hundreds of archives is a list
// nobody finds a game in: a phone shows it as one long wheel or sheet, and the
// folders are headings scrolled past rather than places to go. So a tap on the
// picker opens this window instead, the folders on the left and the games of
// the chosen folder on the right, the way a file manager shows a disk.
//
// Choosing a game sets the select, and nothing else. The select stays what the
// rest of the page reads: it shows the chosen game's name on the main screen as
// it always has, and the start, delete and backup buttons and the speed and
// screen menus follow its value and its change event as before. The window
// reads the folders off the select's own groups when it opens, so there is one
// list in one order, the one listGames built.

// pickerFolders answers the select's groups as folders, in the select's order.
// An option without a value is a message ("no games"), not a game, and a group
// left with no games is not a folder worth opening.
export const pickerFolders = select => [...(select?.children ?? [])]
  .filter(node => node.tagName === "OPTGROUP")
  .map(group => ({
    label: group.label ?? "",
    games: [...(group.children ?? [])]
      .filter(node => node.tagName === "OPTION" && node.value)
      .map(option => ({ value: option.value, name: option.textContent ?? "" })),
  }))
  .filter(folder => folder.games.length > 0);

// folderOf answers the index of the folder holding the game, or the first
// folder when no folder holds it.
export const folderOf = (folders, value) =>
  Math.max(0, folders.findIndex(folder => folder.games.some(game => game.value === value)));

// initGameBrowser wires the button that lies over the picker to the window.
// A page without the window's parts keeps the select's own list.
export const initGameBrowser = ({ document } = {}) => {
  const select = document?.getElementById("game-select");
  const opener = document?.getElementById("game-browse");
  const dialog = document?.getElementById("game-browser");
  const folderList = document?.getElementById("game-browser-folders");
  const gameList = document?.getElementById("game-browser-games");
  const closer = document?.getElementById("game-browser-close");
  if (!select || !opener || !dialog || !folderList || !gameList) return;

  let folders = [];
  let shown = 0;

  const close = () => {
    if (dialog.open) dialog.close();
  };

  // A list scrolls itself to put the button in its middle. Not scrollIntoView,
  // which scrolls every box around the button, the page under the window too.
  const center = (list, button) => {
    if (!button) return;
    list.scrollTop = Math.max(0, button.offsetTop - (list.clientHeight - button.offsetHeight) / 2);
  };

  // A game is chosen by its path. The list can be rebuilt while the window is
  // open (a game added from the page reloads it), so a path the select no
  // longer offers is not set: a select given a value it has no option for
  // selects nothing, and every button reading it would act on no game.
  const choose = value => {
    close();
    if (select.disabled || value === select.value) return;
    if (![...select.options].some(option => option.value === value)) return;
    select.value = value;
    select.dispatchEvent(new Event("change"));
  };

  const entry = (list, onChoose, current) => {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    if (current) button.setAttribute("aria-current", "true");
    button.addEventListener("click", onChoose);
    item.appendChild(button);
    list.appendChild(item);
    return button;
  };

  const showFolders = () => {
    folderList.replaceChildren();
    let current = null;
    folders.forEach((folder, index) => {
      const button = entry(folderList, () => {
        shown = index;
        showFolders();
        showGames();
      }, index === shown);
      const count = document.createElement("span");
      count.className = "game-browser-count";
      count.textContent = String(folder.games.length);
      button.append(folder.label, count);
      if (index === shown) current = button;
    });
    return current;
  };

  // The chosen game is marked and scrolled to, so the window opens where the
  // player already is; another folder opens at its top.
  const showGames = () => {
    gameList.replaceChildren();
    let current = null;
    for (const game of folders[shown]?.games ?? []) {
      const chosen = game.value === select.value;
      const button = entry(gameList, () => choose(game.value), chosen);
      button.textContent = game.name;
      if (chosen) current = button;
    }
    gameList.scrollTop = 0;
    center(gameList, current);
  };

  opener.addEventListener("click", () => {
    if (select.disabled) return;
    folders = pickerFolders(select);
    if (!folders.length) return;
    shown = folderOf(folders, select.value);
    // Shown before it is filled: a list that is not on screen has nothing to
    // scroll to the chosen game.
    dialog.showModal();
    center(folderList, showFolders());
    showGames();
  });
  closer?.addEventListener("click", close);
  // The window's contents fill it edge to edge, so a click whose target is the
  // dialog itself landed on the dimmed page around it.
  dialog.addEventListener("click", event => {
    if (event.target === dialog) close();
  });
};
