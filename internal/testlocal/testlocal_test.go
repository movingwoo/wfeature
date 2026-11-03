package testlocal

import "testing"

const variable = "WFEATURE_TESTLOCAL_SELF_CHECK"

func TestEnabledRunsOnlyWhenTheVariableIsOne(t *testing.T) {
	for value, runs := range map[string]bool{"": false, "0": false, "yes": false, "1": true} {
		t.Setenv(variable, value)
		ran := false
		t.Run("value="+value, func(t *testing.T) {
			Enabled(t, variable, "check the gate")
			ran = true
		})
		if ran != runs {
			t.Errorf("%s=%q: ran %v, want %v", variable, value, ran, runs)
		}
	}
}

func TestValueSkipsWhenUnsetAndAnswersTheValue(t *testing.T) {
	t.Setenv(variable, "")
	t.Run("unset", func(t *testing.T) {
		Value(t, variable, "a local archive")
		t.Error("Value did not skip an empty variable")
	})
	t.Setenv(variable, "/some/archive.zip")
	t.Run("set", func(t *testing.T) {
		if got := Value(t, variable, "a local archive"); got != "/some/archive.zip" {
			t.Errorf("Value() = %q, want the variable's value", got)
		}
	})
}

func TestIntFallsBackWhenUnsetAndReadsAPositiveValue(t *testing.T) {
	t.Setenv(variable, "")
	if got := Int(t, variable, 7); got != 7 {
		t.Errorf("Int() with the variable unset = %d, want the fallback 7", got)
	}
	t.Setenv(variable, "300")
	if got := Int(t, variable, 7); got != 300 {
		t.Errorf("Int() = %d, want 300", got)
	}
}
