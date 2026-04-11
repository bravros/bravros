package license

import (
	"encoding/hex"
	"testing"
)

func TestMachineID_NonEmpty(t *testing.T) {
	id := MachineID()
	if id == "" {
		t.Fatal("MachineID() returned empty string")
	}
}

func TestMachineID_ValidHex(t *testing.T) {
	id := MachineID()
	// SHA-256 produces 32 bytes = 64 hex chars
	if len(id) != 64 {
		t.Errorf("MachineID() length = %d, want 64", len(id))
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("MachineID() is not valid hex: %v", err)
	}
}

func TestMachineID_Deterministic(t *testing.T) {
	a := MachineID()
	b := MachineID()
	if a != b {
		t.Errorf("MachineID() is not deterministic: %q != %q", a, b)
	}
}
