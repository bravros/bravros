//go:build personal

package unifi

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client is a UniFi Dream Machine REST API client.
type Client struct {
	Host      string
	BaseURL   string
	http      *http.Client
	csrfToken string
	cookieJar http.CookieJar
}

// APIResponse wraps the standard UniFi API response envelope.
type APIResponse struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		RC  string `json:"rc"`
		Msg string `json:"msg"`
	} `json:"meta"`
}

// sessionFiles returns paths for cookie and CSRF cache files.
func sessionFiles(host string) (cookieFile, csrfFile string) {
	cookieFile = fmt.Sprintf("/tmp/unifi_cookie_%s.txt", host)
	csrfFile = fmt.Sprintf("/tmp/unifi_csrf_%s.txt", host)
	return
}

// savedCookie is the JSON-serializable form of http.Cookie.
type savedCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Path   string `json:"path"`
	Domain string `json:"domain"`
}

// saveCookies persists the cookie jar to disk for session reuse.
func saveCookies(jar http.CookieJar, baseURL, cookieFile string) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return
	}
	cookies := jar.Cookies(u)
	var saved []savedCookie
	for _, c := range cookies {
		saved = append(saved, savedCookie{
			Name:   c.Name,
			Value:  c.Value,
			Path:   c.Path,
			Domain: c.Domain,
		})
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return
	}
	_ = os.WriteFile(cookieFile, data, 0600)
}

// loadCookies restores cookies from disk into the jar.
func loadCookies(jar http.CookieJar, baseURL, cookieFile string) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return
	}
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		return
	}
	var saved []savedCookie
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	var cookies []*http.Cookie
	for _, s := range saved {
		cookies = append(cookies, &http.Cookie{
			Name:    s.Name,
			Value:   s.Value,
			Path:    s.Path,
			Domain:  s.Domain,
			Expires: time.Now().Add(24 * time.Hour),
		})
	}
	jar.SetCookies(u, cookies)
}

// NewClient creates a new UniFi client, reusing cached session if available.
func NewClient() (*Client, error) {
	host := os.Getenv("UNIFI_HOST")
	if host == "" {
		host = "10.0.0.1"
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// InsecureSkipVerify required for UDM self-signed certs (equivalent to curl -k)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	baseURL := fmt.Sprintf("https://%s", host)

	c := &Client{
		Host:      host,
		BaseURL:   baseURL,
		cookieJar: jar,
		http: &http.Client{
			Jar:       jar,
			Transport: transport,
		},
	}

	// Restore cached session (cookies + CSRF)
	cookieFile, csrfFile := sessionFiles(host)
	loadCookies(jar, baseURL, cookieFile)
	if data, err := os.ReadFile(csrfFile); err == nil {
		c.csrfToken = strings.TrimSpace(string(data))
	}

	return c, nil
}

// getCredentials fetches UniFi credentials from 1Password.
func getCredentials() (string, string, error) {
	usernameOut, err := exec.Command("op", "read", "op://HomeLab/Unifi Claude Api/username").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to read username from 1Password: %w", err)
	}
	passwordOut, err := exec.Command("op", "read", "op://HomeLab/Unifi Claude Api/password").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to read password from 1Password: %w", err)
	}
	return strings.TrimSpace(string(usernameOut)), strings.TrimSpace(string(passwordOut)), nil
}

// Login authenticates with the UniFi controller and caches the session.
func (c *Client) Login() error {
	username, password, err := getCredentials()
	if err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})

	req, err := http.NewRequest("POST", c.BaseURL+"/api/auth/login", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Extract CSRF token from response headers
	if csrf := resp.Header.Get("X-Csrf-Token"); csrf != "" {
		c.csrfToken = csrf
	}

	// Cache session (cookies + CSRF)
	cookieFile, csrfFile := sessionFiles(c.Host)
	saveCookies(c.cookieJar, c.BaseURL, cookieFile)
	_ = os.WriteFile(csrfFile, []byte(c.csrfToken), 0600)

	return nil
}

// EnsureAuth checks if the current session is valid; if not, re-authenticates.
func (c *Client) EnsureAuth() error {
	// Try a quick self-check
	req, err := http.NewRequest("GET", c.BaseURL+"/proxy/network/api/s/default/self", nil)
	if err != nil {
		return c.Login()
	}
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.http.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return c.Login()
	}
	resp.Body.Close()
	return nil
}

// Get performs an authenticated GET request and returns the raw body.
func (c *Client) Get(path string) ([]byte, error) {
	if err := c.EnsureAuth(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s failed (HTTP %d): %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

// Post performs an authenticated POST request with a JSON body.
func (c *Client) Post(path string, data interface{}) ([]byte, error) {
	if err := c.EnsureAuth(); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST %s failed (HTTP %d): %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

// Put performs an authenticated PUT request with a JSON body.
func (c *Client) Put(path string, data interface{}) ([]byte, error) {
	if err := c.EnsureAuth(); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("PUT", c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.csrfToken != "" {
		req.Header.Set("X-Csrf-Token", c.csrfToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PUT %s failed (HTTP %d): %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

// ParseData extracts the "data" array from a standard UniFi API response.
func ParseData(raw []byte) ([]map[string]interface{}, error) {
	var resp APIResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// CheckMeta verifies the API response meta.rc is "ok".
func CheckMeta(raw []byte) error {
	var resp APIResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	if resp.Meta.RC != "ok" {
		return fmt.Errorf("API error: rc=%s msg=%s", resp.Meta.RC, resp.Meta.Msg)
	}
	return nil
}
