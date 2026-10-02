export const QUICK_SAVE = "QUICK_SAVE";
export const QUICK_LOAD = "QUICK_LOAD";
export const isCheckpointKey = name => name === QUICK_SAVE || name === QUICK_LOAD;

// Checkpoints are local keypad actions. Delegation keeps edited and duplicated
// cells working without attaching another listener whenever the layout changes.
export const initCheckpoints = ({ document, session, state, editing,
  onLoaded, onError, setTimer = setTimeout, clearTimer = clearTimeout }) => {
  const note = document.getElementById("checkpoint-status");
  let capable = false;
  let available = false;
  let busy = false;
  let timer;
  const feedback = (message, failed = false, duration = 0) => {
    clearTimer(timer);
    if (!note) return;
    note.textContent = message;
    note.hidden = !message;
    note.classList.toggle("error", failed);
    if (duration) timer = setTimer(() => feedback(""), duration);
  };
  const refresh = () => {
    const playing = state() === "playing" && capable;
    for (const name of [QUICK_SAVE, QUICK_LOAD]) {
      for (const button of document.querySelectorAll(`button[data-key="${name}"]`)) {
        // Unavailable actions must still be selectable and removable in the editor.
        button.disabled = !editing() && (busy || !playing || (name === QUICK_LOAD && !available));
      }
    }
    if (!playing || editing()) feedback("");
  };
  const run = async name => {
    if (busy || editing() || document.hidden || state() !== "playing" || !capable) return;
    if (name === QUICK_LOAD && !available) return;
    busy = true;
    refresh();
    const saving = name === QUICK_SAVE;
    feedback(saving ? "퀵세이브 저장 중…" : "퀵세이브 불러오는 중…");
    try {
      if (saving) {
        await session().quickSave();
        available = true;
      } else {
        const response = await session().quickLoad();
        onLoaded(response.started);
      }
      feedback(saving ? "퀵세이브를 저장했습니다." : "퀵세이브를 불러왔습니다.", false, 2000);
    } catch (error) {
      feedback(`${saving ? "퀵세이브" : "퀵로드"} 실패: ${error instanceof Error ? error.message : String(error)}`, true, 8000);
      onError(error);
    }
    finally { busy = false; refresh(); }
  };
  document.addEventListener("click", event => {
    const button = event.target?.closest?.("button[data-key]");
    if (button && !button.disabled && isCheckpointKey(button.dataset.key)) return run(button.dataset.key);
  });
  refresh();
  return {
    refresh,
    started(info) {
      capable = info?.can_checkpoint === true;
      available = info?.has_checkpoint === true;
      refresh();
    },
  };
};
