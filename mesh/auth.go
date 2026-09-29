package mesh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// TokenSource gives the credential sent as "Authorization: Bearer <token>".
// Two kinds: a declared agent name, for a mesh on the same machine that trusts
// it, and an OAuth token proven by an identity provider, for a gateway that
// verifies it.
type TokenSource interface {
	Token() (string, error)
}

// AgentName is the credential mesh7 accepts without an identity provider:
// "agent:<name>". The mesh trusts the name; fine on one's own machine.
type AgentName string

// Token returns "agent:<name>".
func (a AgentName) Token() (string, error) { return "agent:" + string(a), nil }

// StaticToken is a bearer handed over by whoever launched the agent: a chat
// that exchanged the human's token for one naming the agent and the human
// (RFC 8693). The agent does not refresh it; a run is shorter than its life.
type StaticToken string

// Token returns the token.
func (s StaticToken) Token() (string, error) { return string(s), nil }

// ClientCredentials fetches an OAuth access token with the client credentials
// grant (an agent acting for itself, no human behind it) and keeps it until
// shortly before it expires.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
	HTTP         *http.Client

	mu      sync.Mutex // guards token and expires across concurrent calls
	token   string
	expires time.Time
}

// NewClientCredentials reads the secret from the environment variable named in
// secretEnv, so it never sits in the config file.
func NewClientCredentials(tokenURL, clientID, secretEnv string, scopes []string) (*ClientCredentials, error) {
	secret := os.Getenv(secretEnv)
	if secret == "" {
		return nil, fmt.Errorf("oidc: environment variable %s is empty", secretEnv)
	}
	return &ClientCredentials{
		TokenURL: tokenURL, ClientID: clientID, ClientSecret: secret, Scopes: scopes,
		HTTP: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Token returns the cached token, or fetches a new one when it is missing or
// expires within 30 seconds.
func (c *ClientCredentials) Token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expires) > 30*time.Second {
		return c.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}
	if len(c.Scopes) > 0 {
		form.Set("scope", strings.Join(c.Scopes, " "))
	}
	resp, err := c.HTTP.PostForm(c.TokenURL, form)
	if err != nil {
		return "", fmt.Errorf("oidc: token request: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("oidc: token response (%d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		return "", fmt.Errorf("oidc: token refused (%d): %s %s", resp.StatusCode, body.Error, body.Description)
	}

	c.token = body.AccessToken
	c.expires = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	return c.token, nil
}
