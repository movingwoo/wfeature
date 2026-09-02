// Adding a game from the page.
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
