// Package spotify provides a minimal Spotify API client for track search and
// playlist management. Authentication uses the Authorization Code flow with a
// pre-obtained refresh token stored as an environment variable — no per-user
// OAuth at runtime.
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Track is a Spotify track returned from search results.
type Track struct {
	ID         string
	Name       string
	ArtistName string // first credited artist
}

// Client is a Spotify API client. It is safe for concurrent use.
type Client struct {
	clientID     string
	clientSecret string
	playlistID   string
	httpClient   *http.Client

	mu          sync.Mutex
	refreshToken string
	accessToken  string
	tokenExpiry  time.Time
}

// New creates a Client with the given credentials.
func New(clientID, clientSecret, refreshToken, playlistID string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
		playlistID:   playlistID,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// ensureToken obtains or refreshes the access token when needed. Must be called
// before any API request. Protected by a mutex so concurrent callers only
// refresh once. Falls back to Client Credentials when no refresh token is set
// (search-only; no playlist write scope).
func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return nil
	}
	var data url.Values
	if c.refreshToken != "" {
		data = url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {c.refreshToken},
		}
	} else {
		data = url.Values{"grant_type": {"client_credentials"}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://accounts.spotify.com/api/token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("spotify token refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("spotify token refresh: status %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("spotify token parse: %w", err)
	}
	c.accessToken = tok.AccessToken
	// Subtract 60s so we refresh before the actual expiry.
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	return nil
}

// SearchTracks queries Spotify for up to 5 tracks matching q.
func (c *Client) SearchTracks(ctx context.Context, q string) ([]Track, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	reqURL := "https://api.spotify.com/v1/search?type=track&limit=5&q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spotify search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("spotify search: status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Tracks struct {
			Items []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
			} `json:"items"`
		} `json:"tracks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("spotify search parse: %w", err)
	}

	tracks := make([]Track, 0, len(result.Tracks.Items))
	for _, item := range result.Tracks.Items {
		t := Track{ID: item.ID, Name: item.Name}
		if len(item.Artists) > 0 {
			t.ArtistName = item.Artists[0].Name
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

// SyncPlaylist replaces the contents of the configured playlist with trackIDs
// in order. Returns an error if trackIDs is empty.
func (c *Client) SyncPlaylist(ctx context.Context, trackIDs []string) error {
	if len(trackIDs) == 0 {
		return fmt.Errorf("no tracks to sync")
	}
	if err := c.ensureToken(ctx); err != nil {
		return err
	}

	uris := make([]string, len(trackIDs))
	for i, id := range trackIDs {
		uris[i] = "spotify:track:" + id
	}
	body, err := json.Marshal(map[string]any{"uris": uris})
	if err != nil {
		return err
	}

	reqURL := "https://api.spotify.com/v1/playlists/" + c.playlistID + "/items"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("spotify sync: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("spotify sync: status %d: %s", resp.StatusCode, b)
	}
	return nil
}
