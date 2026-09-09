// The page's own confirmation window.
//
// This is `window.confirm`'s job, and `window.confirm` is not dependable where
// it matters most. The phone app is a WebView with a `WebChromeClient` of its
// own — it has to be, because the file picker behind ＋ 게임 추가 needs one —
// and a client that does not implement `onJsConfirm` decides what a JavaScript
// confirm does. What it must never do is answer "no" with nothing on screen,
// which from the player's side is a button that does nothing at all.
//
// So the question is asked in the page, where the answer is ours: the same
// markup, the same backdrop and the same words on a desktop browser, a phone
// browser, and the app.
//
// It is deliberately plain — one line of text and two buttons, the safe one
// first. The button that goes through carries the verb rather than "확인", so
// that the last thing read before a file goes is what is about to happen to
// it.

// askToConfirm shows the question and answers true only if the person pressed
// the button that goes through. Escape and the backdrop are "no", as is a page
// with no dialog in it at all: a caller that cannot ask has not been answered,
// and a destructive default is the one thing this must not have.
export const askToConfirm = ({ document, message, confirmLabel = "확인" } = {}) =>
  new Promise(resolve => {
    const dialog = document?.getElementById("confirm-dialog");
    const text = document?.getElementById("confirm-text");
    const accept = document?.getElementById("confirm-accept");
    const cancel = document?.getElementById("confirm-cancel");
    if (!dialog || !text || !accept || !cancel) {
      resolve(false);
      return;
    }

    text.textContent = message ?? "";
    accept.textContent = confirmLabel;
    dialog.classList.add("visible");
    // The safe button takes the focus, so a stray Enter is a "no". A phone
    // has no keyboard to press it with, and this is for the desktop where it
    // is the second half of the same guard.
    cancel.focus?.();

    const answer = accepted => {
      dialog.classList.remove("visible");
      accept.removeEventListener("click", onAccept);
      cancel.removeEventListener("click", onCancel);
      document.removeEventListener?.("keydown", onKey, true);
      backdrop?.removeEventListener?.("click", onCancel);
      resolve(accepted);
    };
    const onAccept = () => answer(true);
    const onCancel = () => answer(false);
    // Capture, because the page's own key handling is bound to the document
    // for the keypad: the dialog is what Escape is for while it is open.
    const onKey = event => {
      if (event.key === "Escape") {
        event.stopPropagation();
        answer(false);
      }
    };

    const backdrop = document.querySelector?.(".modal-backdrop");
    accept.addEventListener("click", onAccept);
    cancel.addEventListener("click", onCancel);
    document.addEventListener?.("keydown", onKey, true);
    backdrop?.addEventListener?.("click", onCancel);
  });
