const safeDestination = value => {
  if (typeof value !== "string") return "";
  try {
    const parsed = new URL(value);
    if (!["http:", "https:"].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password) return "";
    return parsed.href;
  } catch {
    return "";
  }
};

// The guest hands the Host a destination and continues independently. This
// notice is deliberately nonmodal: it offers a real link the person may choose
// while leaving guest input and native text-dialog ownership unchanged.
export const createExternalLaunchNotice = ({ document, getSession } = {}) => {
  const notice = document?.getElementById("external-launch-notice");
  const destination = document?.getElementById("external-launch-url");
  const link = document?.getElementById("external-launch-link");
  const dismiss = document?.getElementById("external-launch-dismiss");
  let owner = null, request = 0;

  const hide = (acknowledge, preserveTarget = false) => {
    const connection = owner, token = request;
    owner = null;
    request = 0;
    notice?.classList.remove("visible");
    if (!preserveTarget) {
      if (destination) destination.textContent = "";
      link?.removeAttribute?.("href");
    }
    if (acknowledge && connection && token !== 0) {
      void connection.acknowledgeExternalLaunch(token).catch(() => {});
    }
  };

  const show = handedOff => {
    const token = handedOff?.request;
    const normalized = safeDestination(handedOff?.url);
    const connection = getSession?.();
    if (!notice || !destination || !link || !connection ||
        !Number.isSafeInteger(token) || token <= 0 || !normalized) return false;
    if (owner === connection && request === token) return true;
    owner = connection;
    request = token;
    // What is shown is exactly what the anchor will ask the browser to open.
    // Guest text is assigned as text and never interpreted as markup.
    destination.textContent = normalized;
    link.href = normalized;
    notice.classList.add("visible");
    return true;
  };

  // The anchor's default action preserves a browser user gesture. The Host
  // acknowledgement is fire-and-forget and cannot turn this into automatic
  // navigation if the request fails or the connection has gone away.
  // Keep href in place through the anchor's default action. The hidden
  // notice is overwritten or cleared before it can be used a second time.
  link?.addEventListener("click", () => hide(true, true));
  dismiss?.addEventListener("click", () => hide(true));
  // Focused link/button key presses belong to browser controls. Stop keydown
  // before the handset mapping without preventing native activation. Keyup is
  // left to the document so a guest key held before focus moved is released.
  notice?.addEventListener("keydown", event => event.stopPropagation());

  return {
    show,
    dismiss: () => hide(true),
    // Parking and disconnect remove only this page's copy. The standing Host
    // request is announced again when a page resumes ownership.
    detach: () => hide(false),
  };
};
