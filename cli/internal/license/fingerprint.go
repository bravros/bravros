package license

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"runtime"
)

// MachineID returns a stable, unique identifier for the current machine.
// It is computed as the SHA-256 hex digest of: hostname + first non-loopback
// MAC address + GOOS + GOARCH.
func MachineID() string {
	hostname, _ := os.Hostname()
	mac := firstNonLoopbackMAC()
	raw := hostname + "|" + mac + "|" + runtime.GOOS + "|" + runtime.GOARCH
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

// firstNonLoopbackMAC returns the hardware address of the first non-loopback,
// non-zero network interface, or an empty string if none is found.
func firstNonLoopbackMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr.String()
	}
	return ""
}
