// Carrying a save off this machine, and bringing one back.
//
// The game runs on the server and the save tree is beside it, so from here the
// player's progress is somewhere they cannot point at. On a desktop that is
// only opaque. On a phone it is unreachable: Android since 11 does not let a
// file manager open the folder an app keeps its files in, which is the same
// wall that made a game have to arrive through the page (add-game.js). A player
// who reinstalls, or moves from their phone to a desktop, has no way to bring
// their progress with them and no way to keep a copy of it anywhere.
//
// So the save travels as one file, the way a game does, over the socket the
// page already has. The file is self-describing — it says which game it belongs
// to and whether it arrived whole — and the server checks both before anything
// reaches the tree. What that container is and why it refuses what it refuses
// is written in `internal/backend/savepack.go`; this file is the two buttons.
//
// The server's refusals are written for this screen and are shown as they
// arrive, the way an upload's are: "this backup belongs to a different game"
// and "this backup is damaged" are different things for a person to do next,
// and rewording them here would flatten them back into one.

// The extension the server writes and the input accepts. It is not `.zip`
// because the file is not one, and a name that says so keeps somebody from
// unpacking it, editing it, and finding the checksum refuses it.
export const SAVE_BACKUP_EXTENSION = ".wfs";

const endpoint = game => `api/savepack?game=${encodeURIComponent(game)}`;

// said reads whatever the server put in the body, since those sentences are
// written for this screen. Anything else gets one of our own.
const said = async (response, fallback) => {
  const text = await response.text().catch(() => "");
  return text.trim() || `${fallback} (${response.status})`;
};

// backupFileName is what the downloaded file is called: the game's own name
// with the extension swapped. The path arrived percent-encoded from
// `games.json`, so it is decoded before it becomes a file name.
export const backupFileName = game => {
  let name = String(game ?? "").split("/").pop() ?? "";
  try {
    name = decodeURIComponent(name);
  } catch {
    // A path that is not valid percent-encoding is still a usable name; it
    // came from this server's own listing either way.
  }
  return `${name.replace(/\.(zip|jar)$/i, "") || "save"}${SAVE_BACKUP_EXTENSION}`;
};

// exportSave asks for one game's saves and answers the bytes, or throws with
// what the server said.
export const exportSave = async (game, fetcher = fetch) => {
  const response = await fetcher(endpoint(game), { cache: "no-store" });
  if (!response.ok) throw new Error(await said(response, "세이브를 내보내지 못했습니다"));
  return response.blob();
};

// importSave sends one container back and answers what the restore did, or
// throws with what the server said.
export const importSave = async (game, file, fetcher = fetch) => {
  const response = await fetcher(endpoint(game), { method: "POST", body: file });
  if (!response.ok) throw new Error(await said(response, "세이브를 가져오지 못했습니다"));
  return response.json();
};

// describeImport is the sentence afterwards. The counts are said rather than
// hidden behind "완료": a restore that removed four entries did something a
// restore that removed none did not, and only the person looking knows whether
// that was what they meant.
export const describeImport = ({ written = 0, removed = 0 } = {}) => {
  const parts = [`세이브 ${written}개를 복원했습니다.`];
  if (removed > 0) parts.push(`백업에 없는 ${removed}개는 지웠습니다.`);
  parts.push("게임을 다시 시작하면 적용됩니다.");
  return parts.join(" ");
};

// saveAs hands the browser a file. It is the same three lines the cheat table
// export uses; the object URL is revoked because a page that stays open for a
// session of play would otherwise hold every export it made.
const saveAs = (blob, name, doc) => {
  const link = doc.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = name;
  link.click();
  URL.revokeObjectURL(link.href);
};

// **A web view is not a browser about this.** `<a download>` and a `blob:` URL
// are a request that the host decides what to do with, and a desktop browser
// decides to save the file. A web view embedded in an app decides nothing
// unless the app tells it to: Android's ignores the click outright without a
// DownloadListener, and WKWebView drops the navigation without a download
// delegate. Neither reports anything, so the button read as broken on both
// phones while working everywhere else — which is the platform this feature
// exists for, since a phone is where a player cannot reach the save tree at
// all.
//
// So on a phone the page stops asking for a download and hands the bytes over
// instead, and the app puts up the system's own "where shall I put this"
// — a create-document picker on Android, a document picker on iOS. That is
// also the better answer: a download folder is a place a file lands, and this
// is a file somebody means to keep.
//
// The two hosts are one contract, name and base64, because there is nothing
// about this that differs between them and two shapes would be two things to
// keep in step. Base64 rather than bytes because both bridges carry strings.
const nativeSaver = win => {
  const android = win?.wfeatureExport;
  if (typeof android?.save === "function") {
    return (name, data) => android.save(name, data);
  }
  const ios = win?.webkit?.messageHandlers?.wfeatureExport;
  if (typeof ios?.postMessage === "function") {
    return (name, data) => ios.postMessage({ name, data });
  }
  return null;
};

// base64 of a blob, in chunks: spreading a whole array into fromCharCode
// overflows the argument stack at a size a save can reach.
const toBase64 = async blob => {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  }
  return btoa(binary);
};

// initSaveBackup wires the two buttons.
//
// `chosenGame` is asked at the moment a button is pressed rather than read
// once: the picker is what says which game these buttons act on, and a player
// changes it between pressing them.
//
// The import asks before it runs, because it replaces what is there. That
// confirmation is the page's job rather than the server's: the server cannot
// tell a restore the player asked for from one they reached by picking the
// wrong file, and it is the only step at which the answer is still theirs.
export const initSaveBackup = ({
  document: doc,
  window: win = globalThis,
  fetcher = fetch,
  chosenGame,
  onStatus,
  confirm: ask = globalThis.confirm?.bind(globalThis),
} = {}) => {
  const exportButton = doc.getElementById("save-export");
  const importButton = doc.getElementById("save-import");
  const input = doc.getElementById("save-import-file");
  if (!exportButton || !importButton || !input) return;

  // While one of these is in flight both are disabled, so a second press
  // cannot start an import over an export of the same directory.
  const busy = async (button, label, work) => {
    const previous = button.textContent;
    exportButton.disabled = true;
    importButton.disabled = true;
    button.textContent = label;
    try {
      await work();
    } catch (error) {
      onStatus?.(error.message);
    } finally {
      button.textContent = previous;
      exportButton.disabled = false;
      importButton.disabled = false;
    }
  };

  exportButton.addEventListener("click", () => {
    const game = chosenGame?.();
    if (!game) {
      onStatus?.("먼저 게임을 고르세요.");
      return;
    }
    busy(exportButton, "내보내는 중...", async () => {
      const blob = await exportSave(game, fetcher);
      const name = backupFileName(game);
      const native = nativeSaver(win);
      if (native) {
        native(name, await toBase64(blob));
        // The picker is the app's now, and what happens at it — a place
        // chosen, or a cancel — is not something the page hears about. So it
        // says what it did rather than claiming the file was written.
        onStatus?.("저장할 위치를 고르세요.");
        return;
      }
      saveAs(blob, name, doc);
      onStatus?.("세이브를 내려받았습니다.");
    });
  });

  importButton.addEventListener("click", () => {
    if (!chosenGame?.()) {
      onStatus?.("먼저 게임을 고르세요.");
      return;
    }
    input.click();
  });

  input.addEventListener("change", () => {
    const file = input.files?.[0];
    // Picking the same file twice in a row has to be two changes rather than
    // one: re-importing after fixing something is a thing people do.
    input.value = "";
    if (!file) return;
    const game = chosenGame?.();
    if (!game) {
      onStatus?.("먼저 게임을 고르세요.");
      return;
    }
    if (ask && !ask("이 게임의 저장 데이터를 백업 파일로 되돌립니다. 지금 저장된 진행은 사라집니다. 계속할까요?")) return;
    busy(importButton, "가져오는 중...", async () => {
      onStatus?.(describeImport(await importSave(game, file, fetcher)));
    });
  });
};
