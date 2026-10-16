// A request that never reached the server.
//
// A server that refuses answers with a response, and every caller here shows
// what that response says. A request that never arrived — the server stopped,
// the Wi-Fi dropped — rejects instead, with the browser's own sentence:
// "Failed to fetch", "Load failed", "NetworkError when attempting to fetch
// resource.", in English and different in every browser. That is nothing a
// player can act on, so it is replaced by the one thing it means. The
// browser's error stays on as the cause, for the console.
export const UNREACHABLE = "서버에 연결하지 못했습니다. 연결을 확인한 뒤 다시 시도하세요.";

export const reach = async (fetcher, url, options) => {
  try {
    return await fetcher(url, options);
  } catch (error) {
    throw new Error(UNREACHABLE, { cause: error });
  }
};
