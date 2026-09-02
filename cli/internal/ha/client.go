package ha

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Client is a Home Assistant REST API client.
type Client struct {
	Server string
	Token  string
	http   *http.Client
}

// NewClient creates a new HA client from environment variables.
func NewClient() (*Client, error) {
	server := os.Getenv("HASS_SERVER")
	if server == "" {
		server = "http://homeassistant.local:8123"
	}
	token := os.Getenv("HASS_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("HASS_TOKEN not set — add it to ~/.zshrc")
	}
	return &Client{
		Server: server,
		Token:  token,
		http:   &http.Client{},
	}, nil
}

// BuildTTSPayload builds the JSON payload for a notify/alexa_media TTS call.
// A non-empty target (a media_player entity ID) is what makes delivery rename-proof.
// useTTS=false → "announce" mode (Alexa chime + speech, default UX safeguard).
// useTTS=true  → "tts" mode (silent prefix, no chime). Message is JSON-escaped.
func BuildTTSPayload(message string, useTTS bool, target string) string {
	msgType := "announce"
	if useTTS {
		msgType = "tts"
	}
	escaped, _ := json.Marshal(message)
	if target == "" {
		return fmt.Sprintf(`{"message":%s,"data":{"type":"%s"}}`, string(escaped), msgType)
	}
	escapedTarget, _ := json.Marshal(target)
	return fmt.Sprintf(`{"message":%s,"target":%s,"data":{"type":"%s"}}`,
		string(escaped), string(escapedTarget), msgType)
}

// CallService calls a HA service with JSON data.
func (c *Client) CallService(service, jsonData string) (string, error) {
	url := fmt.Sprintf("%s/api/services/%s", c.Server, service)
	req, err := http.NewRequest("POST", url, strings.NewReader(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// HA answers a successful service call with 200/201. Anything else — 401 stale token,
	// 404 unknown service, 500 integration fault — previously reached callers as a nil error
	// with the body discarded, so `ha say` printed "Sent" for calls that never landed. Surface
	// it instead. NOTE: a 2xx still only means HA accepted the call; a target Echo in Do Not
	// Disturb silently drops the announcement, which no status code can reveal.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return string(body), fmt.Errorf("%s returned %s: %s", service, resp.Status, snippet)
	}
	return string(body), nil
}

// GetState gets the state of an entity.
func (c *Client) GetState(entityID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/states/%s", c.Server, entityID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("home assistant returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetStateValue returns just the "state" field of an entity.
func (c *Client) GetStateValue(entityID string) (string, error) {
	result, err := c.GetState(entityID)
	if err != nil {
		return "", err
	}
	if s, ok := result["state"].(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("no state found")
}

// MacUnlockEntity returns the HA entity ID used for Mac unlock detection.
// Override via HASS_MAC_UNLOCK_ENTITY env var; falls back to the default.
// Both cli/internal/ha and cli/cmd/ha use this function so the entity is
// configured in one place.
func MacUnlockEntity() string {
	if e := os.Getenv("HASS_MAC_UNLOCK_ENTITY"); e != "" {
		return e
	}
	return "input_boolean.macstudio_is_unlocked"
}

// IsMacUnlocked checks if the Mac is unlocked (presence detection).
func (c *Client) IsMacUnlocked() bool {
	state, err := c.GetStateValue(MacUnlockEntity())
	if err != nil {
		return false
	}
	return state == "on"
}
