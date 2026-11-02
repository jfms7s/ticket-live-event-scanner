package envconfig

import (
	"fmt"
	"os"
	"reflect"
	"testing"
)

// fatalCalled is the panic value the test fatalf raises.
type fatalCalled string

// expectFatal runs f with fatalf replaced and reports the fatal message, or
// "" when f returned normally.
func expectFatal(t *testing.T, f func()) (msg string) {
	t.Helper()
	old := fatalf
	fatalf = func(format string, args ...any) { panic(fatalCalled(fmt.Sprintf(format, args...))) }
	defer func() {
		fatalf = old
		if r := recover(); r != nil {
			m, ok := r.(fatalCalled)
			if !ok {
				panic(r)
			}
			msg = string(m)
		}
	}()
	f()
	return ""
}

func TestString(t *testing.T) {
	t.Setenv("ENVCONFIG_S", "")
	if got := String("ENVCONFIG_S", "def"); got != "def" {
		t.Errorf("unset: got %q, want def", got)
	}
	t.Setenv("ENVCONFIG_S", "v")
	if got := String("ENVCONFIG_S", "def"); got != "v" {
		t.Errorf("set: got %q, want v", got)
	}
}

func TestRequired(t *testing.T) {
	t.Setenv("ENVCONFIG_R", "v")
	if msg := expectFatal(t, func() {
		if got := Required("ENVCONFIG_R"); got != "v" {
			t.Errorf("got %q, want v", got)
		}
	}); msg != "" {
		t.Errorf("unexpected fatal: %s", msg)
	}
	t.Setenv("ENVCONFIG_R", "")
	if msg := expectFatal(t, func() { Required("ENVCONFIG_R") }); msg == "" {
		t.Error("missing required variable did not stop the process")
	}
}

func TestInt(t *testing.T) {
	const key = "ENVCONFIG_I"
	tests := []struct {
		name      string
		value     string
		want      int
		wantFatal bool
	}{
		{"unset uses the default", "", 587, false},
		{"valid", "465", 465, false},
		{"zero", "0", 0, false},
		{"surrounding spaces", " 25 ", 25, false},
		{"unparsable", "1.5s", 0, true},
		{"embedded space", "58 7", 0, true},
		{"negative", "-1", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(key, tt.value)
			var got int
			msg := expectFatal(t, func() { got = Int(key, 587) })
			if tt.wantFatal {
				if msg == "" {
					t.Fatalf("Int(%q) = %d, want a fatal error", tt.value, got)
				}
				return
			}
			if msg != "" {
				t.Fatalf("unexpected fatal: %s", msg)
			}
			if got != tt.want {
				t.Fatalf("Int(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestIntFatalNamesVariableAndValue(t *testing.T) {
	t.Setenv("REQUEST_DELAY_MS", "1.5s")
	msg := expectFatal(t, func() { Int("REQUEST_DELAY_MS", 1500) })
	if msg != `environment variable REQUEST_DELAY_MS="1.5s" is not an integer` {
		t.Errorf("fatal message = %q", msg)
	}
}

func TestBool(t *testing.T) {
	const key = "ENVCONFIG_B"
	for _, tc := range []struct {
		value string
		set   bool
		want  bool
	}{
		{"", false, false},
		{"true", true, true},
		{"TRUE", true, true},
		{"1", true, true},
		{"false", true, false},
		{"0", true, false},
		{"yes", true, false},
	} {
		if tc.set {
			t.Setenv(key, tc.value)
		} else {
			os.Unsetenv(key)
		}
		if got := Bool(key); got != tc.want {
			t.Errorf("Bool(%q set=%v) = %v, want %v", tc.value, tc.set, got, tc.want)
		}
	}
}

func TestList(t *testing.T) {
	const key = "ENVCONFIG_L"
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"single", "a@example.com", []string{"a@example.com"}},
		{"multiple", "a@example.com,b@example.com", []string{"a@example.com", "b@example.com"}},
		{"whitespace trimmed", " a@example.com , b@example.com ", []string{"a@example.com", "b@example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.value)
			if got := List(key); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("List(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
	for _, v := range []string{"", " , ,"} {
		t.Setenv(key, v)
		if msg := expectFatal(t, func() { List(key) }); msg == "" {
			t.Errorf("List(%q) did not stop the process", v)
		}
	}
}

func TestNatsURL(t *testing.T) {
	t.Setenv("NATS_URL", "")
	if got := NatsURL(); got != DefaultNatsURL {
		t.Errorf("unset: got %q", got)
	}
	t.Setenv("NATS_URL", "nats://nats:4222")
	if got := NatsURL(); got != "nats://nats:4222" {
		t.Errorf("set: got %q", got)
	}
}
