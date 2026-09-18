// The OS owns composition. Only an explicit submission sends complete text.
const explain = error => {
  switch (error?.message) {
    case "no supported text field is active": return "게임에서 입력할 칸을 먼저 선택해 주세요. 게임 자체 입력창은 지원되지 않을 수 있습니다.";
    case "the active text field changed; open text input again": return "게임의 입력칸이 바뀌었습니다. 닫고 다시 열어 주세요.";
    case "text does not satisfy the active field constraints": return "입력 가능한 문자와 길이를 확인해 주세요.";
    default: return "문자 입력에 실패했습니다. 연결 상태를 확인해 주세요.";
  }
};

export const createTextInputDialog = ({ document, getSession, releaseInput }) => {
  const node = name => document.getElementById(`text-input-${name}`);
  const dialog = node("dialog"), status = node("status"), single = node("value"), multi = node("multiline");
  const apply = node("apply"), cancel = node("cancel"), hint = node("hint");
  let generation = 0, owner = null, edit = null, field = single, composing = false, busy = false;
  const showStatus = (state, message, guidance = "") => {
    status.dataset.state = state;
    status.textContent = message;
    status.hidden = false;
    hint.textContent = guidance;
    hint.hidden = !guidance;
  };
  const showError = error => {
    const stale = error?.message === "the active text field changed; open text input again";
    const unavailable = edit == null || stale;
    showStatus(unavailable ? "unavailable" : "error",
      unavailable ? "지금은 입력할 수 없습니다" : "입력 내용을 확인해 주세요", explain(error));
    apply.disabled = unavailable;
    if (stale) field.readOnly = true;
  };
  const discard = (connection, token) => {
    if (connection && token != null) void connection.cancelTextInput(token).catch(() => {});
  };
  const close = () => {
    generation++;
    discard(owner, edit);
    owner = null;
    edit = null;
    single.value = multi.value = "";
    composing = busy = false;
    showStatus("checking", "입력칸을 확인하고 있습니다…");
    single.readOnly = multi.readOnly = false;
    apply.textContent = "입력";
    cancel.textContent = "닫기";
    if (dialog.open) dialog.close();
  };
  const open = async () => {
    close();
    const current = generation, connection = getSession();
    if (!connection) return;
    owner = connection;
    releaseInput();
    single.hidden = multi.hidden = true;
    field = single;
    single.type = "text";
    single.disabled = multi.disabled = true;
    apply.disabled = true;
    dialog.showModal();
    try {
      const { textInput } = await connection.openTextInput();
      if (current !== generation) { discard(connection, textInput?.edit); return; }
      if (!textInput || !Number.isSafeInteger(textInput.edit)) throw new Error("invalid text input response");
      edit = textInput.edit;
      // Password contents must never be placed in a visible textarea.
      field = textInput.multiline && !textInput.password ? multi : single;
      single.hidden = field !== single;
      multi.hidden = field !== multi;
      single.type = textInput.password ? "password" : "text";
      field.inputMode = textInput.inputMode || "text";
      field.value = textInput.text;
      field.disabled = false;
      apply.disabled = false;
      apply.textContent = textInput.append ? "삽입" : "입력";
      cancel.textContent = "취소";
      const guidance = textInput.append
        ? "입력한 문자를 게임의 커서 위치에 추가합니다."
        : "입력 버튼을 누르면 게임의 입력칸에 반영됩니다.";
      showStatus("available", "입력할 수 있습니다", guidance);
      // Phones may require a fresh tap after the asynchronous response.
      field.focus();
    } catch (error) {
      if (current === generation) showError(error);
    }
  };
  apply.addEventListener("click", async () => {
    if (edit == null || busy || composing || field.readOnly) return;
    const current = generation;
    busy = true;
    apply.disabled = true;
    field.disabled = true;
    showStatus("checking", "게임에 반영하고 있습니다…");
    try {
      await owner.commitTextInput(edit, field.value);
      if (current === generation) { edit = null; close(); }
    } catch (error) {
      if (current === generation) {
        showError(error);
        busy = false;
        field.disabled = false;
      }
    }
  });
  cancel.addEventListener("click", close);
  dialog.addEventListener("cancel", event => {
    event.preventDefault();
    if (!composing) close();
  });
  dialog.addEventListener("compositionstart", () => { composing = true; });
  dialog.addEventListener("compositionend", () => { composing = false; });
  // Keep dialog buttons and IME keys out of the emulator's document listener.
  dialog.addEventListener("keydown", event => event.stopPropagation());
  dialog.addEventListener("keyup", event => event.stopPropagation());
  return { open, close };
};
