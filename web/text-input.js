// The OS owns composition. Only an explicit submission sends complete text.
export const createTextInputDialog = ({ document, getSession, releaseInput }) => {
  const node = name => document.getElementById(`text-input-${name}`);
  const dialog = node("dialog"), status = node("status"), single = node("value"), multi = node("multiline");
  const apply = node("apply"), cancel = node("cancel");
  let generation = 0, owner = null, edit = null, field = single, composing = false, busy = false;
  const showStatus = state => {
    status.dataset.state = state;
    status.hidden = state === "checking";
    status.textContent = status.hidden ? "" : state === "available" ? "입력 가능" : "입력 불가능";
  };
  const showError = error => {
    const stale = error?.message === "the active text field changed; open text input again";
    const unavailable = edit == null || stale;
    showStatus(unavailable ? "unavailable" : "error");
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
    showStatus("checking");
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
      showStatus("available");
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
    showStatus("checking");
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
