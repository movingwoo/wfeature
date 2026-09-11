package backend

// AuthenticationStatus reports whether a requested offline authentication
// adaptation matched this session. It does not describe server access control
// or prove that the guest reached gameplay.
type AuthenticationStatus string

const (
	AuthenticationOff              AuthenticationStatus = "off"
	AuthenticationUnsupported      AuthenticationStatus = "unsupported"
	AuthenticationKTFCertificate23 AuthenticationStatus = "ktf-certificate-23"
	AuthenticationKTFCertificate52 AuthenticationStatus = "ktf-certificate-52"
	AuthenticationSKTLicense       AuthenticationStatus = "skt-license"
	AuthenticationKTFSubscriber    AuthenticationStatus = "ktf-subscriber-fallback"
	AuthenticationLGTOptions       AuthenticationStatus = "lgt-cached-authentication"
	AuthenticationLGTCertificate58 AuthenticationStatus = "lgt-certificate-58"
	AuthenticationLGTNotification  AuthenticationStatus = "lgt-offline-notification"
)
