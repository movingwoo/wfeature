package lgt

import "testing"

// Setting a handset property is refused, as KTF refuses it from Java and from
// C: the answer is false and what getSystemProperty answers stays the table's.
func TestJavaHandsetPropertyRefusesToSetOne(t *testing.T) {
	const (
		setProperty = javaHandsetPropertyClass + ".setSystemProperty(Ljava/lang/String;Ljava/lang/String;)Z"
		getProperty = javaHandsetPropertyClass + ".getSystemProperty(Ljava/lang/String;)Ljava/lang/String;"
	)
	client := fixtureClient(t)
	name := newTestString(t, client, "MEDIADEVICES")
	if got := mustJavaCall(t, client, setProperty, name, newTestString(t, client, "something else")); got != javaFalse {
		t.Errorf("setSystemProperty answered %d, want false", got)
	}
	if got := mustJavaCall(t, client, setProperty, 0, 0); got != javaFalse {
		t.Errorf("setSystemProperty(null, null) answered %d, want false", got)
	}
	value, ok := client.javaText(mustJavaCall(t, client, getProperty, name))
	if want, _ := client.systemProperty("MEDIADEVICES"); !ok || value != want {
		t.Errorf("after the refused set the property reads %q, want the table's %q", value, want)
	}
}

// The backlight is not there to drive, so asking for it for a while, turning
// it off and putting back what was there before are each accepted and do
// nothing, as alwaysOn already is — what KTF's and SKT's BackLight do.
func TestJavaBackLightControlsAreAccepted(t *testing.T) {
	client := fixtureClient(t)
	const backLight = "org/kwis/msp/handset/BackLight"
	mustJavaCall(t, client, backLight+".on(III)V", 100, 5000, 1)
	mustJavaCall(t, client, backLight+".off()V")
	mustJavaCall(t, client, backLight+".before()V")
	mustJavaCall(t, client, backLight+".alwaysOn()V")
}
