//go:build personal

package ha

// DeviceMap maps friendly names to HA notify service paths.
var DeviceMap = map[string]string{
	"studio":   "notify/alexa_media_echo_studio",
	"sala":     "notify/alexa_media_echo_dot_sala",
	"suite":    "notify/alexa_media_echo_show_suite",
	"banheiro": "notify/alexa_media_echo_banheiro_suite",
	"gourmet":  "notify/alexa_media_echo_area_gourmet",
	"todos":    "notify/alexa_media_todo_lugar",
}

// ColorMap maps color names to RGB arrays.
var ColorMap = map[string][3]int{
	"blue":   {0, 0, 255},
	"red":    {255, 0, 0},
	"green":  {0, 255, 0},
	"yellow": {255, 200, 0},
	"white":  {255, 255, 255},
}

// StudioLights are the entity IDs for studio light group.
var StudioLights = []string{
	"light.teto_do_estudio",
	"light.luz_teto_estudio_armario",
	"light.luz_teto_estudio",
}

// ResolveDevice returns the HA service path for a device name.
func ResolveDevice(name string) string {
	if svc, ok := DeviceMap[name]; ok {
		return svc
	}
	return "notify/alexa_media_" + name
}
