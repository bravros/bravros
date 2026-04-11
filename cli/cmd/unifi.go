//go:build personal

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/bravros/private/internal/unifi"
	"github.com/spf13/cobra"
)

// prettyJSON marshals v with 2-space indent and prints it.
func unifiPrint(v interface{}) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ JSON marshal error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// unifiClient creates a new client or exits on error.
func unifiClient() *unifi.Client {
	c, err := unifi.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	return c
}

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getBool safely extracts a bool from a map.
func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getFloat safely extracts a float64 from a map.
func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// findUserID looks up a user_id for a given MAC address from stat/sta or rest/user.
func findUserID(client *unifi.Client, mac string) (string, string, error) {
	mac = strings.ToLower(mac)

	// Try stat/sta first
	raw, err := client.Get("/proxy/network/api/s/default/stat/sta")
	if err == nil {
		data, err := unifi.ParseData(raw)
		if err == nil {
			for _, d := range data {
				if strings.ToLower(getString(d, "mac")) == mac {
					userID := getString(d, "user_id")
					if userID == "" {
						userID = getString(d, "_id")
					}
					return userID, getString(d, "network_id"), nil
				}
			}
		}
	}

	// Try rest/user
	raw, err = client.Get("/proxy/network/api/s/default/rest/user")
	if err == nil {
		data, err := unifi.ParseData(raw)
		if err == nil {
			for _, d := range data {
				if strings.ToLower(getString(d, "mac")) == mac {
					return getString(d, "_id"), getString(d, "network_id"), nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("client with MAC %s not found", mac)
}

var unifiCmd = &cobra.Command{
	Use:   "unifi",
	Short: "UniFi network management CLI",
	Long: `unifi — UniFi Dream Machine network management CLI

  Auth:
    unifi login                              Authenticate with UDM

  Clients:
    unifi clients                            List connected clients
    unifi client <mac>                       Client details by MAC
    unifi known-clients                      List all known clients
    unifi search <term>                      Search clients by hostname/mac/ip/oui

  Client Management:
    unifi rename <mac> <name>                Rename a client
    unifi kick <mac>                         Disconnect a client
    unifi block <mac>                        Block a client
    unifi unblock <mac>                      Unblock a client

  DHCP:
    unifi dhcp-leases                        List fixed IP assignments
    unifi set-fixed-ip <mac> <ip> [net_id]   Assign fixed IP
    unifi remove-fixed-ip <mac>              Remove fixed IP

  Devices:
    unifi devices                            List network devices
    unifi device <mac>                       Device details by MAC
    unifi switch-ports <mac>                 Switch port table
    unifi reboot <mac>                       Reboot a device
    unifi power-cycle <mac> <port>           PoE power cycle a port
    unifi locate <mac>                       Enable locate LED
    unifi unlocate <mac>                     Disable locate LED

  Firmware:
    unifi firmware-check                     Check for firmware updates
    unifi firmware-upgrade <mac>             Upgrade device firmware

  Network:
    unifi networks                           List networks
    unifi port-profile                       List port profiles
    unifi system                             System info

  Advanced:
    unifi raw <GET|POST|PUT> <path> [data]   Direct API call`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var unifiLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with UDM",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		if err := c.Login(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		unifiPrint(map[string]string{
			"status": "authenticated",
			"host":   c.Host,
		})
	},
}

var unifiClientsCmd = &cobra.Command{
	Use:   "clients",
	Short: "List connected clients",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/stat/sta")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			hostname := getString(d, "hostname")
			if hostname == "" {
				hostname = getString(d, "name")
			}
			if hostname == "" {
				hostname = "unknown"
			}
			ip := getString(d, "ip")
			if ip == "" {
				ip = "N/A"
			}
			result = append(result, map[string]interface{}{
				"mac":      getString(d, "mac"),
				"ip":       ip,
				"hostname": hostname,
				"oui":      getString(d, "oui"),
				"is_wired": getBool(d, "is_wired"),
				"network":  getString(d, "last_connection_network_name"),
				"uplink":   getString(d, "last_uplink_name"),
				"fixed_ip": getBool(d, "use_fixedip"),
				"user_id":  getString(d, "user_id"),
			})
		}

		sort.Slice(result, func(i, j int) bool {
			return strings.ToLower(getString(result[i], "hostname")) < strings.ToLower(getString(result[j], "hostname"))
		})

		unifiPrint(result)
	},
}

var unifiClientCmd = &cobra.Command{
	Use:   "client <mac>",
	Short: "Client details by MAC",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])
		raw, err := c.Get("/proxy/network/api/s/default/stat/sta")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		for _, d := range data {
			if strings.ToLower(getString(d, "mac")) == mac {
				unifiPrint(d)
				return
			}
		}
		fmt.Fprintf(os.Stderr, "❌ Client with MAC %s not found\n", mac)
		os.Exit(1)
	},
}

var unifiKnownClientsCmd = &cobra.Command{
	Use:   "known-clients",
	Short: "List all known clients",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/rest/user")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			name := getString(d, "name")
			if name == "" {
				name = getString(d, "hostname")
			}
			if name == "" {
				name = "unknown"
			}
			result = append(result, map[string]interface{}{
				"id":          getString(d, "_id"),
				"mac":         getString(d, "mac"),
				"name":        name,
				"hostname":    getString(d, "hostname"),
				"fixed_ip":    getString(d, "fixed_ip"),
				"use_fixedip": getBool(d, "use_fixedip"),
				"is_guest":    getBool(d, "is_guest"),
				"noted":       getBool(d, "noted"),
				"blocked":     getBool(d, "blocked"),
			})
		}

		sort.Slice(result, func(i, j int) bool {
			return strings.ToLower(getString(result[i], "name")) < strings.ToLower(getString(result[j], "name"))
		})

		unifiPrint(result)
	},
}

var unifiSearchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Search clients by hostname/mac/ip/oui",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		term := strings.ToLower(args[0])
		raw, err := c.Get("/proxy/network/api/s/default/stat/sta")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			hn := getString(d, "hostname")
			if hn == "" {
				hn = getString(d, "name")
			}
			hostname := strings.ToLower(hn)
			mac := strings.ToLower(getString(d, "mac"))
			ip := strings.ToLower(getString(d, "ip"))
			oui := strings.ToLower(getString(d, "oui"))

			if strings.Contains(hostname, term) || strings.Contains(mac, term) ||
				strings.Contains(ip, term) || strings.Contains(oui, term) {
				if hn == "" {
					hn = "unknown"
				}
				ipVal := getString(d, "ip")
				if ipVal == "" {
					ipVal = "N/A"
				}
				result = append(result, map[string]interface{}{
					"mac":      getString(d, "mac"),
					"ip":       ipVal,
					"hostname": hn,
					"oui":      getString(d, "oui"),
					"is_wired": getBool(d, "is_wired"),
					"network":  getString(d, "last_connection_network_name"),
					"user_id":  getString(d, "user_id"),
				})
			}
		}

		unifiPrint(result)
	},
}

var unifiRenameCmd = &cobra.Command{
	Use:   "rename <mac> <name>",
	Short: "Rename a client",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])
		name := args[1]

		userID, _, err := findUserID(c, mac)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		raw, err := c.Put(
			fmt.Sprintf("/proxy/network/api/s/default/rest/user/%s", userID),
			map[string]string{"name": name},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status": "ok",
			"mac":    mac,
			"name":   name,
		})
	},
}

var unifiDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List network devices",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/stat/device")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			result = append(result, map[string]interface{}{
				"mac":     getString(d, "mac"),
				"ip":      getString(d, "ip"),
				"name":    getString(d, "name"),
				"model":   getString(d, "model"),
				"type":    getString(d, "type"),
				"version": getString(d, "version"),
				"uptime":  getFloat(d, "uptime"),
				"status":  getFloat(d, "state"),
				"num_sta": getFloat(d, "num_sta"),
			})
		}

		unifiPrint(result)
	},
}

var unifiDeviceCmd = &cobra.Command{
	Use:   "device <mac>",
	Short: "Device details by MAC",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])
		raw, err := c.Get("/proxy/network/api/s/default/stat/device")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		for _, d := range data {
			if strings.ToLower(getString(d, "mac")) == mac {
				unifiPrint(d)
				return
			}
		}
		fmt.Fprintf(os.Stderr, "❌ Device with MAC %s not found\n", mac)
		os.Exit(1)
	},
}

var unifiSwitchPortsCmd = &cobra.Command{
	Use:   "switch-ports <mac>",
	Short: "Switch port table",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])
		raw, err := c.Get("/proxy/network/api/s/default/stat/device")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		for _, d := range data {
			if strings.ToLower(getString(d, "mac")) == mac {
				portTable, ok := d["port_table"].([]interface{})
				if !ok {
					fmt.Fprintf(os.Stderr, "❌ No port table for device %s\n", mac)
					os.Exit(1)
				}

				var result []map[string]interface{}
				for _, p := range portTable {
					port, ok := p.(map[string]interface{})
					if !ok {
						continue
					}
					result = append(result, map[string]interface{}{
						"port_idx":   getFloat(port, "port_idx"),
						"name":       getString(port, "name"),
						"media":      getString(port, "media"),
						"speed":      getFloat(port, "speed"),
						"up":         getBool(port, "up"),
						"poe_enable": getBool(port, "poe_enable"),
						"poe_power":  getString(port, "poe_power"),
						"rx_bytes":   getFloat(port, "rx_bytes"),
						"tx_bytes":   getFloat(port, "tx_bytes"),
					})
				}
				unifiPrint(result)
				return
			}
		}
		fmt.Fprintf(os.Stderr, "❌ Device with MAC %s not found\n", mac)
		os.Exit(1)
	},
}

var unifiNetworksCmd = &cobra.Command{
	Use:   "networks",
	Short: "List networks",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/rest/networkconf")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			result = append(result, map[string]interface{}{
				"id":           getString(d, "_id"),
				"name":         getString(d, "name"),
				"purpose":      getString(d, "purpose"),
				"subnet":       getString(d, "ip_subnet"),
				"vlan":         getString(d, "vlan"),
				"dhcp_enabled": getBool(d, "dhcpd_enabled"),
				"dhcp_start":   getString(d, "dhcpd_start"),
				"dhcp_stop":    getString(d, "dhcpd_stop"),
				"domain_name":  getString(d, "domain_name"),
			})
		}

		unifiPrint(result)
	},
}

var unifiDHCPLeasesCmd = &cobra.Command{
	Use:   "dhcp-leases",
	Short: "List fixed IP assignments",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/rest/user")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			if getBool(d, "use_fixedip") {
				result = append(result, map[string]interface{}{
					"mac":        getString(d, "mac"),
					"name":       getString(d, "name"),
					"fixed_ip":   getString(d, "fixed_ip"),
					"network_id": getString(d, "network_id"),
				})
			}
		}

		sort.Slice(result, func(i, j int) bool {
			return getString(result[i], "fixed_ip") < getString(result[j], "fixed_ip")
		})

		unifiPrint(result)
	},
}

var unifiSetFixedIPCmd = &cobra.Command{
	Use:   "set-fixed-ip <mac> <ip> [network_id]",
	Short: "Assign fixed IP to client",
	Args:  cobra.RangeArgs(2, 3),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])
		ip := args[1]

		userID, networkID, err := findUserID(c, mac)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		if len(args) > 2 {
			networkID = args[2]
		}
		if networkID == "" {
			fmt.Fprintf(os.Stderr, "❌ network_id required — pass as third argument\n")
			os.Exit(1)
		}

		payload := map[string]interface{}{
			"use_fixedip": true,
			"fixed_ip":    ip,
			"network_id":  networkID,
		}

		raw, err := c.Put(
			fmt.Sprintf("/proxy/network/api/s/default/rest/user/%s", userID),
			payload,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]interface{}{
			"status":      "ok",
			"mac":         mac,
			"hostname":    mac,
			"fixed_ip":    ip,
			"use_fixedip": true,
		})
	},
}

var unifiRemoveFixedIPCmd = &cobra.Command{
	Use:   "remove-fixed-ip <mac>",
	Short: "Remove fixed IP from client",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		userID, _, err := findUserID(c, mac)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		raw, err := c.Put(
			fmt.Sprintf("/proxy/network/api/s/default/rest/user/%s", userID),
			map[string]interface{}{"use_fixedip": false},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Fixed IP removed for %s", mac),
		})
	},
}

var unifiKickCmd = &cobra.Command{
	Use:   "kick <mac>",
	Short: "Disconnect a client",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/stamgr", map[string]string{
			"cmd": "kick-sta",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Client %s disconnected", mac),
			"mac":     mac,
		})
	},
}

var unifiBlockCmd = &cobra.Command{
	Use:   "block <mac>",
	Short: "Block a client",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/stamgr", map[string]string{
			"cmd": "block-sta",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Client %s blocked", mac),
			"mac":     mac,
		})
	},
}

var unifiUnblockCmd = &cobra.Command{
	Use:   "unblock <mac>",
	Short: "Unblock a client",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/stamgr", map[string]string{
			"cmd": "unblock-sta",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Client %s unblocked", mac),
			"mac":     mac,
		})
	},
}

var unifiRebootCmd = &cobra.Command{
	Use:   "reboot <mac>",
	Short: "Reboot a device",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/devmgr", map[string]string{
			"cmd": "restart",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Device %s rebooting", mac),
			"mac":     mac,
		})
	},
}

var unifiPowerCycleCmd = &cobra.Command{
	Use:   "power-cycle <mac> <port>",
	Short: "PoE power cycle a port",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])
		port, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Invalid port number: %s\n", args[1])
			os.Exit(1)
		}

		raw, err := c.Post("/proxy/network/api/s/default/cmd/devmgr", map[string]interface{}{
			"cmd":      "power-cycle",
			"mac":      mac,
			"port_idx": port,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]interface{}{
			"status":  "ok",
			"message": fmt.Sprintf("Power cycled port %d on %s", port, mac),
			"mac":     mac,
			"port":    port,
		})
	},
}

var unifiLocateCmd = &cobra.Command{
	Use:   "locate <mac>",
	Short: "Enable locate LED on device",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/devmgr", map[string]string{
			"cmd": "set-locate",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Locate enabled for %s", mac),
			"mac":     mac,
		})
	},
}

var unifiUnlocateCmd = &cobra.Command{
	Use:   "unlocate <mac>",
	Short: "Disable locate LED on device",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/devmgr", map[string]string{
			"cmd": "unset-locate",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Locate disabled for %s", mac),
			"mac":     mac,
		})
	},
}

var unifiFirmwareCheckCmd = &cobra.Command{
	Use:   "firmware-check",
	Short: "Check for firmware updates",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/stat/device")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			if getBool(d, "upgradable") || getString(d, "upgrade_to_firmware") != "" {
				result = append(result, map[string]interface{}{
					"mac":             getString(d, "mac"),
					"name":            getString(d, "name"),
					"model":           getString(d, "model"),
					"current_version": getString(d, "version"),
					"upgrade_to":      getString(d, "upgrade_to_firmware"),
				})
			}
		}

		if len(result) == 0 {
			unifiPrint(map[string]string{"message": "All devices are up to date"})
		} else {
			unifiPrint(result)
		}
	},
}

var unifiFirmwareUpgradeCmd = &cobra.Command{
	Use:   "firmware-upgrade <mac>",
	Short: "Upgrade device firmware",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		mac := strings.ToLower(args[0])

		raw, err := c.Post("/proxy/network/api/s/default/cmd/devmgr", map[string]string{
			"cmd": "upgrade",
			"mac": mac,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		if err := unifi.CheckMeta(raw); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		unifiPrint(map[string]string{
			"status":  "ok",
			"message": fmt.Sprintf("Firmware upgrade started for %s", mac),
			"mac":     mac,
		})
	},
}

var unifiSystemCmd = &cobra.Command{
	Use:   "system",
	Short: "System info",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/stat/sysinfo")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		if len(data) == 0 {
			fmt.Fprintf(os.Stderr, "❌ No system info returned\n")
			os.Exit(1)
		}

		d := data[0]
		unifiPrint(map[string]interface{}{
			"version":          getString(d, "version"),
			"hostname":         getString(d, "hostname"),
			"name":             getString(d, "name"),
			"uptime":           getFloat(d, "uptime"),
			"update_available": getBool(d, "update_available"),
		})
	},
}

var unifiPortProfileCmd = &cobra.Command{
	Use:   "port-profile",
	Short: "List port profiles",
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		raw, err := c.Get("/proxy/network/api/s/default/rest/portconf")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		data, err := unifi.ParseData(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		var result []map[string]interface{}
		for _, d := range data {
			result = append(result, map[string]interface{}{
				"id":                    getString(d, "_id"),
				"name":                  getString(d, "name"),
				"native_networkconf_id": getString(d, "native_networkconf_id"),
				"poe_mode":              getString(d, "poe_mode"),
				"speed":                 getFloat(d, "speed"),
			})
		}

		unifiPrint(result)
	},
}

var unifiRawCmd = &cobra.Command{
	Use:   "raw <GET|POST|PUT> <path> [data]",
	Short: "Direct API call",
	Args:  cobra.RangeArgs(2, 3),
	Run: func(cmd *cobra.Command, args []string) {
		c := unifiClient()
		method := strings.ToUpper(args[0])
		path := args[1]

		var raw []byte
		var err error

		switch method {
		case "GET":
			raw, err = c.Get(path)
		case "POST":
			var body interface{}
			if len(args) > 2 {
				if jsonErr := json.Unmarshal([]byte(args[2]), &body); jsonErr != nil {
					fmt.Fprintf(os.Stderr, "❌ Invalid JSON data: %v\n", jsonErr)
					os.Exit(1)
				}
			} else {
				body = map[string]interface{}{}
			}
			raw, err = c.Post(path, body)
		case "PUT":
			var body interface{}
			if len(args) > 2 {
				if jsonErr := json.Unmarshal([]byte(args[2]), &body); jsonErr != nil {
					fmt.Fprintf(os.Stderr, "❌ Invalid JSON data: %v\n", jsonErr)
					os.Exit(1)
				}
			} else {
				body = map[string]interface{}{}
			}
			raw, err = c.Put(path, body)
		default:
			fmt.Fprintf(os.Stderr, "❌ Unsupported method: %s (use GET, POST, or PUT)\n", method)
			os.Exit(1)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		// Pretty-print the raw JSON response
		var parsed interface{}
		if jsonErr := json.Unmarshal(raw, &parsed); jsonErr == nil {
			unifiPrint(parsed)
		} else {
			fmt.Println(string(raw))
		}
	},
}

func init() {
	unifiCmd.AddCommand(unifiLoginCmd)
	unifiCmd.AddCommand(unifiClientsCmd)
	unifiCmd.AddCommand(unifiClientCmd)
	unifiCmd.AddCommand(unifiKnownClientsCmd)
	unifiCmd.AddCommand(unifiSearchCmd)
	unifiCmd.AddCommand(unifiRenameCmd)
	unifiCmd.AddCommand(unifiDevicesCmd)
	unifiCmd.AddCommand(unifiDeviceCmd)
	unifiCmd.AddCommand(unifiSwitchPortsCmd)
	unifiCmd.AddCommand(unifiNetworksCmd)
	unifiCmd.AddCommand(unifiDHCPLeasesCmd)
	unifiCmd.AddCommand(unifiSetFixedIPCmd)
	unifiCmd.AddCommand(unifiRemoveFixedIPCmd)
	unifiCmd.AddCommand(unifiKickCmd)
	unifiCmd.AddCommand(unifiBlockCmd)
	unifiCmd.AddCommand(unifiUnblockCmd)
	unifiCmd.AddCommand(unifiRebootCmd)
	unifiCmd.AddCommand(unifiPowerCycleCmd)
	unifiCmd.AddCommand(unifiLocateCmd)
	unifiCmd.AddCommand(unifiUnlocateCmd)
	unifiCmd.AddCommand(unifiFirmwareCheckCmd)
	unifiCmd.AddCommand(unifiFirmwareUpgradeCmd)
	unifiCmd.AddCommand(unifiSystemCmd)
	unifiCmd.AddCommand(unifiPortProfileCmd)
	unifiCmd.AddCommand(unifiRawCmd)
	rootCmd.AddCommand(unifiCmd)
}
