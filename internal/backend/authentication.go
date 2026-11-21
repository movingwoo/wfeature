package backend

// AuthenticationStatus reports whether a requested offline authentication
// adaptation matched this session. It does not describe server access control
// or prove that the guest reached gameplay.
type AuthenticationStatus string

const (
	// AuthenticationGuestManaged leaves the decision to the guest's original checks.
	AuthenticationGuestManaged          AuthenticationStatus = "guest-managed"
	AuthenticationOff                   AuthenticationStatus = "off"
	AuthenticationUnsupported           AuthenticationStatus = "unsupported"
	AuthenticationKTFCertificate23      AuthenticationStatus = "ktf-certificate-23"
	AuthenticationKTFCertificate52      AuthenticationStatus = "ktf-certificate-52"
	AuthenticationSKTLicense            AuthenticationStatus = "skt-license"
	AuthenticationKTFSubscriber         AuthenticationStatus = "ktf-subscriber-fallback"
	AuthenticationKTFSaveSubscriber     AuthenticationStatus = "ktf-save-subscriber"
	AuthenticationLGTOptions            AuthenticationStatus = "lgt-cached-authentication"
	AuthenticationLGTCertificate100     AuthenticationStatus = "lgt-certificate-100"
	AuthenticationLGTCertificate58      AuthenticationStatus = "lgt-certificate-58"
	AuthenticationLGTNotification       AuthenticationStatus = "lgt-offline-notification"
	AuthenticationLGTHandshake          AuthenticationStatus = "lgt-offline-authentication"
	AuthenticationLGTSaveSubscriber     AuthenticationStatus = "lgt-save-subscriber"
	AuthenticationLGTCertificateMessage AuthenticationStatus = "lgt-certificate-message"
	// AuthenticationLGTAuthenticationAllowance is a registered title whose
	// one-time network authentication is given up after its first failure
	// rather than its fifth, as the title itself does after five.
	AuthenticationLGTAuthenticationAllowance AuthenticationStatus = "lgt-authentication-allowance"
	// The SKT native statuses accept one identity comparison in a recognized
	// executable; see docs/skt-native.md#skt-native-identity.
	AuthenticationSKTNativeCertificate    AuthenticationStatus = "skt-native-certificate"
	AuthenticationSKTNativeSaveSubscriber AuthenticationStatus = "skt-native-save-subscriber"
	// AuthenticationSKTScriptInstall accepts a recognized script's comparison
	// of the handset with the user ID its installation was issued.
	AuthenticationSKTScriptInstall AuthenticationStatus = "skt-script-install-identity"
)

// AuthenticationKTFBREWPackagedCertificate selects the archival record
// verifier on a registered BREW module.
const AuthenticationKTFBREWPackagedCertificate AuthenticationStatus = "ktf-brew-packaged-certificate"
