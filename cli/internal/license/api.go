package license

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBaseURL = "https://app.bravros.dev/api/v1"
	userAgent      = "bravros/1.0.0"
)

// ClientIface is the interface satisfied by *Client and any test double.
type ClientIface interface {
	Activate(licenseKey, machineID string) (string, error)
	Verify(token, machineID string) (string, error)
	Deactivate(token, machineID string) error
}

// DefaultClient is the package-level API client used by commands.
// It can be overridden in tests.
var DefaultClient ClientIface = NewClient(defaultBaseURL)


// APIError represents a structured error response from the bravros API.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error %s: %s", e.Code, e.Message)
}

// Client is an HTTP client for the bravros license API.
type Client struct {
	BaseURL    string
	httpClient *http.Client
}

// NewClient creates a new Client with the given base URL and a 10-second timeout.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// apiResponse is the generic envelope for all API responses.
type apiResponse struct {
	Token string `json:"token"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// doPost sends a POST request and returns the parsed response.
func (c *Client) doPost(path string, body interface{}) (*apiResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("license: failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("license: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("license: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("license: failed to decode response: %w", err)
	}

	if result.Error != nil {
		return nil, &APIError{
			Code:    result.Error.Code,
			Message: result.Error.Message,
		}
	}

	return &result, nil
}

// Activate sends a license activation request and returns a JWT on success.
func (c *Client) Activate(licenseKey, machineID string) (string, error) {
	body := map[string]string{
		"license_key": licenseKey,
		"machine_id":  machineID,
	}
	resp, err := c.doPost("/activate", body)
	if err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", fmt.Errorf("license: activate response missing token")
	}
	return resp.Token, nil
}

// Verify sends a token refresh request and returns the refreshed JWT.
func (c *Client) Verify(token, machineID string) (string, error) {
	body := map[string]string{
		"token":      token,
		"machine_id": machineID,
	}
	resp, err := c.doPost("/verify", body)
	if err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", fmt.Errorf("license: verify response missing token")
	}
	return resp.Token, nil
}

// Deactivate sends a deactivation request for the given token and machine.
func (c *Client) Deactivate(token, machineID string) error {
	body := map[string]string{
		"token":      token,
		"machine_id": machineID,
	}
	_, err := c.doPost("/deactivate", body)
	return err
}
