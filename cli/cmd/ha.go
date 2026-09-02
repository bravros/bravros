package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/ha"
	"github.com/spf13/cobra"
)

var haCmd = &cobra.Command{
	Use:   "ha",
	Short: "Home Assistant CLI",
	Long: `ha — Home Assistant CLI

  TTS:
    ha say "message" [device]     Send announcement (Alexa chime + speech); --tts for silent prefix
    ha room [device]              Set which Echo announcements target; --clear to unset
    ha mute [on|off|toggle] [dur] Global kill-switch (Echo + local say); e.g. ha mute on 45m

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
var haSayTTS bool

var haSayCmd = &cobra.Command{
	Use:   "say <message> [device]",
	Short: "Send announcement (Alexa chime + speech) to Echo. Use --tts for silent prefix.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		msg := args[0]
		dev := "studio"
		if len(args) > 1 {
			dev = args[1]
		}
		if err := sendHASay(msg, dev, haSayForce, haSayTTS, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
	},
}

func sendHASay(msg, dev string, force, tts bool, out io.Writer) error {
	// The kill-switch outranks everything, --force included: mute is an explicit operator
	// action for calls and meetings, whereas --force only bypasses the Mac-unlock presence
	// gate. scripts/announce.sh enforces this too, but the check has to live here as well —
	// otherwise a bare `bravros ha say` walks straight past it and the command's own
	// "kill-switch for every announcement" help text is false. Checked before NewClient so a
	// muted host needs no HASS_TOKEN to stay quiet.
	if muted, until := ha.MuteStatus(); muted {
		if until.IsZero() {
			fmt.Fprintf(out, "🔇 Skipped (announcements muted): %s\n", msg)
		} else {
			fmt.Fprintf(out, "🔇 Skipped (muted until %s): %s\n", until.Format("15:04"), msg)
		}
		return nil
	}

	client, err := ha.NewClient()
	if err != nil {
		return err
	}

	// Presence check for studio (skip with --force)
	if !force && dev == "studio" {
		if !client.IsMacUnlocked() {
			fmt.Fprintf(out, "Skipped (Mac locked — not at desk): %s\n", msg)
			return nil
		}
	}

	// Alexa silently discards announcements to a device with Do Not Disturb on: HA returns
	// 200 and nothing plays, so the failure is invisible from here. Clear DND first rather
	// than lose the message. Opt out with BRAVROS_DND_AUTOCLEAR=0.
	if os.Getenv("BRAVROS_DND_AUTOCLEAR") != "0" {
		if slug := ha.DeviceSlug(dev); slug != "" {
			client.CallService("switch/turn_off",
				fmt.Sprintf(`{"entity_id":"switch.%s_do_not_disturb_switch"}`, slug))
		}
	}

	svc := ha.ResolveDevice(dev)
	data := ha.BuildTTSPayload(msg, tts, ha.DeviceTarget(dev))
	if _, err := client.CallService(svc, data); err != nil {
		return fmt.Errorf("delivery to %s failed: %w", dev, err)
	}
	fmt.Fprintf(out, "Sent to %s: %s\n", dev, msg)
	return nil
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

		lightsJSON, _ := json.Marshal(ha.StudioLights())

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
		c := exec.Command("hass-cli", "state", "get", ha.MacUnlockEntity())
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

var haRoomClear bool

var haRoomCmd = &cobra.Command{
	Use:   "room [device]",
	Short: "Set which Echo announcements target (rooms come from ~/.claude/ha-devices.json)",
	Long: `Set the room every announcement targets.

Skill call sites hardcode "studio"; this override redirects them to wherever you actually
are, without editing them. Honoured only on a roaming (Wi-Fi) MacBook — on the Mac Studio,
or on a MacBook with a wired default route, you are at the desk by definition and the
target stays the studio Echo.

State is ~/.claude/.echo-room, also read directly by scripts/announce.sh.`,
	Run: func(cmd *cobra.Command, args []string) {
		if haRoomClear {
			if err := ha.SetRoom(""); err != nil {
				fmt.Fprintf(os.Stderr, "❌ %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Room cleared — announcements honour each call's own device again")
			return
		}
		if len(args) == 0 {
			if room := ha.CurrentRoom(); room != "" {
				fmt.Printf("Room: %s (%s)\n", room, ha.DeviceSlug(room))
			} else {
				fmt.Println("Room: unset — announcements honour each call's own device")
			}
			return
		}
		name := args[0]
		devices := ha.DeviceMap()
		if len(devices) == 0 {
			fmt.Fprintf(os.Stderr, "❌ no device map configured — create %s (see templates/ha-devices.example.json)\n", ha.DevicesFile())
			os.Exit(1)
		}
		if _, known := devices[name]; !known {
			fmt.Fprintf(os.Stderr, "❌ unknown device %q — known: ", name)
			names := make([]string, 0, len(devices))
			for k := range devices {
				names = append(names, k)
			}
			sort.Strings(names)
			fmt.Fprintln(os.Stderr, strings.Join(names, ", "))
			os.Exit(1)
		}
		if err := ha.SetRoom(name); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Room set: %s (%s)\n", name, ha.DeviceSlug(name))
	},
}

var haMuteCmd = &cobra.Command{
	Use:   "mute [on|off|toggle|status] [duration]",
	Short: "Global kill-switch for every announcement (Echo AND local say)",
	Long: `Silence every audio announcement, wherever you are.

Mutes BOTH surfaces — Echo and the local macOS say fallback. That is deliberate: mute is
for calls, where a laptop speaking aloud is picked up by the mic and is worse than the Echo.

Duration accepts 30s / 45m / 2h; omit it to mute indefinitely. A timed mute self-heals on
expiry, so muting before a call can never strand you in permanent silence.

State is ~/.claude/.mute, also read directly by scripts/announce.sh and writable by
scripts/mute-announce.sh (the StreamDeck entry point) — so mute works even on a machine
whose bravros binary is out of date.`,
	Run: func(cmd *cobra.Command, args []string) {
		verb := "status"
		if len(args) > 0 {
			verb = args[0]
		}
		var dur time.Duration
		if len(args) > 1 {
			d, err := time.ParseDuration(args[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ bad duration %q (use 30s, 45m, 2h)\n", args[1])
				os.Exit(1)
			}
			dur = d
		}

		show := func() {
			muted, until := ha.MuteStatus()
			switch {
			case muted && !until.IsZero():
				fmt.Printf("🔇 muted until %s\n", until.Format("15:04"))
			case muted:
				fmt.Println("🔇 muted")
			default:
				fmt.Println("🔊 announcements on")
			}
		}

		var err error
		switch verb {
		case "on", "mute":
			err = ha.SetMute(dur)
		case "off", "unmute":
			err = ha.ClearMute()
		case "toggle":
			if muted, _ := ha.MuteStatus(); muted {
				err = ha.ClearMute()
			} else {
				err = ha.SetMute(dur)
			}
		case "status":
		default:
			fmt.Fprintf(os.Stderr, "❌ unknown verb %q (use on|off|toggle|status)\n", verb)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		show()
	},
}

func init() {
	haSayCmd.Flags().BoolVar(&haSayForce, "force", false, "Bypass presence detection for studio device")
	haSayCmd.Flags().BoolVar(&haSayTTS, "tts", false, "Use plain TTS (no Alexa chime) — opt-out of announce-mode default")
	haCmd.AddCommand(haSayCmd)
	haCmd.AddCommand(haLightsCmd)
	haCmd.AddCommand(haDeskCmd)
	haCmd.AddCommand(haStateCmd)
	haCmd.AddCommand(haListCmd)
	haCmd.AddCommand(haToggleCmd)
	haCmd.AddCommand(haMacCmd)
	haCmd.AddCommand(haReloadCmd)
	haCmd.AddCommand(haSshCmd)
	haRoomCmd.Flags().BoolVar(&haRoomClear, "clear", false, "Clear the room override")
	haCmd.AddCommand(haRoomCmd)
	haCmd.AddCommand(haMuteCmd)
}
