// MODIFICATIONS IGNORED ON GRADESCOPE!

// Client query wrappers that check correctness.

package tests

import (
	"testing"
)

func queryLogf(t *testing.T, verbose bool, format string, a ...any) {
	if verbose {
		t.Logf(format, a...)
	}
}

func get(t *testing.T, verbose bool, client clientWr, key string, expValue string, expOk bool) bool {
	queryLogf(t, verbose, "(%s) Calling client.Get(%q)", client.name, key)
	value, ok, err := client.c.Get(key)
	if err != nil {
		t.Errorf("[ERROR] (%s) Get(%q) returned error: %s", client.name, key, err)
		return false
	}
	queryLogf(t, verbose, "(%s) Get(%q) returned (%q, %t)", client.name, key, value, ok)
	if !expOk && ok {
		t.Errorf("[ERROR] (%s) Get(%q) gave ok=true for non-present value", client.name, key)
		return false
	}
	if expOk && !ok {
		t.Errorf("[ERROR] (%s) Get(%q) gave ok=false, but expected value %q", client.name, key, expValue)
		return false
	}
	if expValue != value {
		t.Errorf("[ERROR] (%s) Get(%q) gave value %q, but expected %q", client.name, key, value, expValue)
		return false
	}
	return true
}

func put(t *testing.T, verbose bool, client clientWr, key string, value string) bool {
	queryLogf(t, verbose, "(%s) Calling client.Put(%q, %q)", client.name, key, value)
	err := client.c.Put(key, value)
	if err != nil {
		t.Errorf("[ERROR] (%s) Put(%q, %q) returned error: %s", client.name, key, value, err)
		return false
	}
	queryLogf(t, verbose, "(%s) Put(%q, %q) succeeded", client.name, key, value)
	return true
}

func list(t *testing.T, verbose bool, client clientWr, prefix string, expEntries map[string]string) bool {
	queryLogf(t, verbose, "(%s) Calling client.List(%q)", client.name, prefix)
	entries, err := client.c.List(prefix)
	if err != nil {
		t.Errorf("[ERROR] (%s) List(%q) returned error: %s", client.name, prefix, err)
		return false
	}
	if entries == nil {
		t.Errorf("[ERROR] (%s) List(%q) returned nil", client.name, prefix)
		return false
	}

	// This is noisy, but you can uncomment to log List outputs.
	// queryLogf(t, "(%s) List(%q) returned %s", client.name, prefix, entries)

	if len(entries) < len(expEntries) {
		// Print out an example missing entry.
		for key, value := range expEntries {
			if _, ok := entries[key]; !ok {
				t.Errorf("[ERROR] (%s) List(%q) omits expected entry (%q, %q)", client.name, prefix, key, value)
				return false
			}
		}
	}
	for key, value := range entries {
		expValue, expOk := expEntries[key]
		if !expOk {
			t.Errorf("[ERROR] (%s) List(%q) has unexpected key %q with value %q", client.name, prefix, key, value)
			return false
		}
		if expValue != value {
			t.Errorf("[ERROR] (%s) List(%q) has key %q with value %q, but expected %q", client.name, prefix, key, value, expValue)
			return false
		}
	}
	return true
}
