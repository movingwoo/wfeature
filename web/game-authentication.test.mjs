import assert from "node:assert/strict";
import { test } from "node:test";
import { authenticationMessage } from "./game-authentication.js";

test("unsupported and applied results are distinguished without claiming gameplay succeeded", () => {
  assert.match(authenticationMessage("unsupported"), /지원하지 않는/);
  assert.match(authenticationMessage("ktf-certificate-23"), /현재 실행/);
  assert.match(authenticationMessage("ktf-certificate-52"), /현재 실행/);
  assert.match(authenticationMessage("lgt-certificate-58"), /현재 실행/);
  assert.match(authenticationMessage("skt-license"), /현재 실행/);
  assert.match(authenticationMessage("ktf-subscriber-fallback"), /현재 실행/);
  assert.match(authenticationMessage("lgt-cached-authentication"), /현재 실행/);
  assert.match(authenticationMessage("lgt-offline-notification"), /로컬 처리/);
  assert.match(authenticationMessage("lgt-offline-notification"), /외부 전송은 하지 않습니다/);
  assert.equal(authenticationMessage("off"), "");
  assert.equal(authenticationMessage(undefined), "");
});
