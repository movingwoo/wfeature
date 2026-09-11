export const authenticationMessage = status => {
  if (status === "ktf-certificate-23" || status === "ktf-certificate-52" || status === "lgt-certificate-58") return "인증 호환: 인증서를 현재 실행에 적용했습니다.";
  if (status === "skt-license") return "인증 호환: 라이선스 검사를 현재 실행에 적용했습니다.";
  if (status === "ktf-subscriber-fallback") return "인증 호환: 게임에 내장된 번호를 현재 실행에 적용했습니다.";
  if (status === "lgt-cached-authentication") return "인증 호환: 저장된 인증 결과를 현재 실행에 적용했습니다.";
  if (status === "lgt-offline-notification") return "인증 호환: 동의 선택 통보·원격 세이브 조회를 로컬 처리합니다. 외부 전송은 하지 않습니다.";
  if (status === "unsupported") return "인증 호환: 아직 지원하지 않는 방식입니다. 기본 동작으로 실행합니다.";
  return "";
};
