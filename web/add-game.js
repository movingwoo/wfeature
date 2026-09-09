// Adding a game from the page, and taking it back off.
//
// A game used to arrive one way: the user put a file in a folder beside the
// server. That is the whole story on a desktop, and no story at all on a
// phone — Android since 11 does not let a file manager open the folder an app
// keeps its files in, so "put the archive here" names a place nobody can
// reach. The file has to travel over the socket the page already has.
//
// It is the same control on every platform, which is the point: a file input
// is answered by the system picker on Android, by the Files sheet on iOS, and
// by the ordinary dialog on a desktop, and none of that is ours to write.

// Taking one back off is the other half of the same route, and for the same
// reason: on a phone the directory the archive lands in is one nothing else
// can open, so a game added by mistake would stay for as long as the app is
// installed. Only the games that came in this way can go — the library beside
// the server is somebody's own, and the server refuses it whatever this page
// asks; see internal/webhost/remove.go.

import { askToConfirm } from "./confirm.js";

// uploadGame sends one archive and answers with nothing, or throws with what
// the server said. The name travels in the query rather than a header because
// these names are Korean and a header is Latin-1.
export const uploadGame = async (file, fetcher = fetch) => {
  const response = await fetcher(`api/games?name=${encodeURIComponent(file.name)}`, {
    method: "POST",
    body: file,
  });
  if (!response.ok) {
    // The server's refusals are written for this screen, so they are shown
    // rather than replaced. Anything else gets a sentence of our own.
    const said = await response.text().catch(() => "");
    throw new Error(said.trim() || `게임을 추가하지 못했습니다 (${response.status})`);
  }
};

// uploadGames sends several and reports what happened to each, because a
// person picking five archives at once should not lose four of them to one
// bad file.
export const uploadGames = async (files, fetcher = fetch) => {
  const added = [];
  const failed = [];
  for (const file of files) {
    try {
      await uploadGame(file, fetcher);
      added.push(file.name);
    } catch (error) {
      failed.push({ name: file.name, reason: error.message });
    }
  }
  return { added, failed };
};

// describe is the sentence the user reads afterwards. One file gets its own
// name; several get a count, since a list of five names is not a status line.
export const describe = ({ added, failed }) => {
  if (added.length === 0 && failed.length === 1) return failed[0].reason;
  const parts = [];
  if (added.length === 1) parts.push(`${added[0]} 을(를) 추가했습니다.`);
  else if (added.length > 1) parts.push(`게임 ${added.length}개를 추가했습니다.`);
  if (failed.length === 1) parts.push(`${failed[0].name}: ${failed[0].reason}`);
  else if (failed.length > 1) parts.push(`${failed.length}개는 추가하지 못했습니다.`);
  return parts.join(" ");
};

// gameFileName is the archive's own name, out of the path `games.json` gave
// the page. It is what a person is asked about before a file is deleted, so it
// is the name they picked rather than the path this page uses.
export const gameFileName = game => {
  const last = String(game ?? "").split("/").pop() ?? "";
  try {
    return decodeURIComponent(last);
  } catch {
    // A path that is not valid percent-encoding is still a usable name; it
    // came from this server's own listing either way.
    return last;
  }
};

// groupLabel is the picker heading a game belongs under. A game added from the
// page has no platform directory to name it — which platform it belongs to is
// read from its bytes when it is loaded, and nothing has read them yet — so it
// is grouped by where it came from, which is also the group the delete button
// works on. A blank heading reads as a glitch, so the archives loose in the
// library get one of their own.
export const groupLabel = game =>
  game.added ? "추가한 게임" : (game.group ? game.group.toUpperCase() : "기타");

// syncRemoveButton puts the delete button in the state of whatever the picker
// is showing. The option carries the answer because the listing did: this page
// does not decide what may be deleted — the server refuses the rest whatever
// the page sends — it only stops offering what it knows will be refused.
export const syncRemoveButton = document => {
  const button = document.getElementById("game-remove");
  if (!button) return;
  const chosen = document.getElementById("game-select")?.selectedOptions?.[0];
  button.disabled = chosen?.dataset?.added !== "yes";
};

// removeGame deletes one added game and answers with nothing, or throws with
// what the server said. The path is the picker's own, the way the save routes
// take it: one spelling of "which game" across the API.
export const removeGame = async (game, fetcher = fetch) => {
  const response = await fetcher(`api/games?game=${encodeURIComponent(game)}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    const said = await response.text().catch(() => "");
    throw new Error(said.trim() || `게임을 지우지 못했습니다 (${response.status})`);
  }
};

// initAddGame wires the button. It reloads the picker itself rather than
// telling the user to, because a game that was added and does not appear reads
// as a failure.
export const initAddGame = ({ document, fetcher = fetch, onAdded, onStatus } = {}) => {
  const input = document.getElementById("game-file");
  const button = document.getElementById("game-add");
  if (!input || !button) return;

  button.addEventListener("click", () => input.click());
  input.addEventListener("change", async () => {
    const files = [...(input.files ?? [])];
    // The value is cleared so that picking the same file twice in a row is
    // two changes rather than one: re-adding a corrected archive is a thing
    // people do.
    input.value = "";
    if (files.length === 0) return;

    const previous = button.textContent;
    button.disabled = true;
    button.textContent = "추가하는 중...";
    try {
      const result = await uploadGames(files, fetcher);
      onStatus?.(describe(result));
      if (result.added.length > 0) await onAdded?.();
    } catch (error) {
      onStatus?.(error.message);
    } finally {
      button.disabled = false;
      button.textContent = previous;
    }
  });
};

// initRemoveGame wires the other button. It asks first — a deletion is the one
// thing on this screen that cannot be undone from it — and it names the file
// in the question, because the picker is a list and the wrong row is the whole
// failure. The saves stay, so the question says so: re-adding the same archive
// comes back to the same progress.
export const initRemoveGame = ({
  document,
  chosenGame,
  fetcher = fetch,
  confirmRemoval = message => askToConfirm({ document, message, confirmLabel: "삭제" }),
  onRemoved,
  onStatus,
} = {}) => {
  const button = document.getElementById("game-remove");
  if (!button) return;

  button.addEventListener("click", async () => {
    const game = chosenGame?.() ?? "";
    if (!game) return;
    const name = gameFileName(game);
    // Awaited, because the question is asked in the page rather than by the
    // browser: window.confirm is not dependable in the app's WebView, and what
    // it would do there is answer "no" with nothing on screen. See confirm.js.
    if (!(await confirmRemoval(`${name} 을(를) 지울까요? 저장된 세이브는 남습니다.`))) return;

    const previous = button.textContent;
    button.disabled = true;
    button.textContent = "지우는 중...";
    try {
      await removeGame(game, fetcher);
      onStatus?.(`${name} 을(를) 지웠습니다.`);
      await onRemoved?.();
    } catch (error) {
      onStatus?.(error.message);
    } finally {
      // The list has been rebuilt by now, and whether this button belongs to
      // whatever the picker landed on is that rebuild's answer rather than
      // this one's — so the state is read back off the picker instead of
      // being restored from before the press. Handing it back enabled is what
      // left it live over a game it may not delete: removing the last added
      // game leaves a library one selected, and the button stayed lit over it.
      button.textContent = previous;
      syncRemoveButton(document);
    }
  });
};
