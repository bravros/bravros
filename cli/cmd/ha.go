//go:build personal

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bravros/private/internal/ha"
	"github.com/spf13/cobra"
)

var haCmd = &cobra.Command{
	Use:   "ha",
	Short: "Home Assistant CLI",
	Long: `ha — Home Assistant CLI

  TTS:
    ha say "message" [device]     Send TTS (studio/sala/suite/banheiro/gourmet/todos)

  Lights:
    ha lights on [color]          Studio lights on (blue/red/green/yellow/white/[r,g,b])
    ha lights off                 Studio lights off
    ha lights status              List all lights

  Desk:
    ha desk up|down|on|off|toggle|timer|status

  Entities:
    ha state <entity>             Get entity state
    ha list [filter]              List entities
    ha toggle <entity>            Toggle an entity

  System:
    ha mac                        Mac lock/unlock status
    ha reload                     Reload automations
    ha ssh [command]              SSH to Home Assistant`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var haSayForce bool

var haSayCmd = &cobra.Command{
	Use:   "say <message> [device]",
	Short: "Send TTS to Alexa",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := ha.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		msg := args[0]
		dev := "studio"
		if len(args) > 1 {
			dev = args[1]
		}

		// Presence check for studio (skip with --force)
		if !haSayForce && dev == "studio" {
			if !client.IsMacUnlocked() {
				fmt.Printf("Skipped (Mac locked — not at desk): %s\n", msg)
				return
			}
		}

		svc := ha.ResolveDevice(dev)
		data := fmt.Sprintf(`{"message":"%s","data":{"type":"tts"}}`, msg)
		client.CallService(svc, data)
		fmt.Printf("Sent to %s: %s\n", dev, msg)
	},
}

var haLightsCmd = &cobra.Command{
	Use:   "lights [on|off|status] [color]",
	Short: "Studio lights control",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := ha.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		action := "status"
		if len(args) > 0 {
			action = args[0]
		}

		lightsJSON, _ := json.Marshal(ha.StudioLights)

		switch action {
		case "on":
			if len(args) > 1 {
				colorName := args[1]
				rgb, ok := ha.ColorMap[colorName]
				if !ok {
					// Treat as raw RGB
					data := fmt.Sprintf(`{"entity_id":%s,"rgb_color":%s,"brightness":255}`, string(lightsJSON), colorName)
					client.CallService("light/turn_on", data)
				} else {
					data := fmt.Sprintf(`{"entity_id":%s,"rgb_color":[%d,%d,%d],"brightness":255}`, string(lightsJSON), rgb[0], rgb[1], rgb[2])
					client.CallService("light/turn_on", data)
				}
				fmt.Printf("Studio lights on (%s)\n", colorName)
			} else {
				data := fmt.Sprintf(`{"entity_id":%s}`, string(lightsJSON))
				client.CallService("light/turn_on", data)
				fmt.Println("Studio lights on")
			}
		case "off":
			data := fmt.Sprintf(`{"entity_id":%s}`, string(lightsJSON))
			client.CallService("light/turn_off", data)
			fmt.Println("Studio lights off")
		case "status":
			c := exec.Command("hass-cli", "state", "list")
			out, err := c.Output()
			if err == nil {
				for _, line := range strings.Split(string(out), "\n") {
					if strings.HasPrefix(line, "light.") {
						fmt.Println(line)
					}
				}
			}
		}
	},
}

var haDeskCmd = &cobra.Command{
	Use:   "desk [up|down|on|off|toggle|timer|status]",
	Short: "Standing desk control",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := ha.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}

		action := "status"
		if len(args) > 0 {
			action = args[0]
		}

		switch action {
		case "up":
			client.CallService("select/select_option", `{"entity_id":"select.mesa_estudio_level","option":"level_2"}`)
			fmt.Println("Desk → level_2 (standing)")
		case "down":
			client.CallService("select/select_option", `{"entity_id":"select.mesa_estudio_level","option":"level_1"}`)
			fmt.Println("Desk → level_1 (sitting)")
		case "on":
			client.CallService("input_boolean/turn_on", `{"entity_id":"input_boolean.standing_desk_enabled"}`)
			fmt.Println("Standing desk reminders ON")
		case "off":
			client.CallService("input_boolean/turn_off", `{"entity_id":"input_boolean.standing_desk_enabled"}`)
			fmt.Println("Standing desk reminders OFF")
		case "toggle":
			client.CallService("input_boolean/toggle", `{"entity_id":"input_boolean.standing_desk_enabled"}`)
			fmt.Println("Standing desk toggled")
		case "timer":
			state, err := client.GetState("sensor.standing_desk_countdown")
			if err == nil {
				unit := ""
				if attrs, ok := state["attributes"].(map[string]interface{}); ok {
					if u, ok := attrs["unit_of_measurement"].(string); ok {
						unit = u
					}
				}
				fmt.Printf("Countdown: %v %s\n", state["state"], unit)
			}
		case "status":
			exec.Command("hass-cli", "state", "get", "select.mesa_estudio_level").Run()
			exec.Command("hass-cli", "state", "get", "input_boolean.standing_desk_enabled").Run()
			state, err := client.GetState("sensor.standing_desk_countdown")
			if err == nil {
				unit := ""
				if attrs, ok := state["attributes"].(map[string]interface{}); ok {
					if u, ok := attrs["unit_of_measurement"].(string); ok {
						unit = u
					}
				}
				fmt.Printf("Timer: %v %s\n", state["state"], unit)
			}
		}
	},
}

var haStateCmd = &cobra.Command{
	Use:   "state <entity_id>",
	Short: "Get entity state",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := exec.Command("hass-cli", "state", "get", args[0])
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Run()
	},
}

var haListCmd = &cobra.Command{
	Use:   "list [filter]",
	Short: "List entities",
	Run: func(cmd *cobra.Command, args []string) {
		c := exec.Command("hass-cli", "state", "list")
		out, err := c.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to list entities\n")
			return
		}
		if len(args) > 0 {
			filter := strings.ToLower(args[0])
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(strings.ToLower(line), filter) {
					fmt.Println(line)
				}
			}
		} else {
			fmt.Print(string(out))
		}
	},
}

var haToggleCmd = &cobra.Command{
	Use:   "toggle <entity_id>",
	Short: "Toggle an entity",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client, err := ha.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		entity := args[0]
		domain := strings.Split(entity, ".")[0]
		client.CallService(domain+"/toggle", fmt.Sprintf(`{"entity_id":"%s"}`, entity))
		fmt.Printf("Toggled %s\n", entity)
	},
}

var haMacCmd = &cobra.Command{
	Use:   "mac",
	Short: "Mac lock/unlock status",
	Run: func(cmd *cobra.Command, args []string) {
		c := exec.Command("hass-cli", "state", "get", "input_boolean.macstudio_is_unlocked")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Run()
	},
}

var haReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload automations",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := ha.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		client.CallService("automation/reload", "{}")
		fmt.Println("Automations reloaded")
	},
}

var haSshCmd = &cobra.Command{
	Use:   "ssh [command...]",
	Short: "SSH to Home Assistant",
	Run: func(cmd *cobra.Command, args []string) {
		sshArgs := append([]string{"homeassistant"}, args...)
		c := exec.Command("ssh", sshArgs...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Run()
	},
}

func init() {
	haSayCmd.Flags().BoolVar(&haSayForce, "force", false, "Bypass presence detection for studio device")
	haCmd.AddCommand(haSayCmd)
	haCmd.AddCommand(haLightsCmd)
	haCmd.AddCommand(haDeskCmd)
	haCmd.AddCommand(haStateCmd)
	haCmd.AddCommand(haListCmd)
	haCmd.AddCommand(haToggleCmd)
	haCmd.AddCommand(haMacCmd)
	haCmd.AddCommand(haReloadCmd)
	haCmd.AddCommand(haSshCmd)
	rootCmd.AddCommand(haCmd)
}
