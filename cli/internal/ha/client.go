//go:build personal

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

// IsMacUnlocked checks if the Mac is unlocked (presence detection).
func (c *Client) IsMacUnlocked() bool {
	state, err := c.GetStateValue("input_boolean.macstudio_is_unlocked")
	if err != nil {
		return false
	}
	return state == "on"
}
