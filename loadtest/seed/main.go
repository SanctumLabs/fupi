// Command seed registers load-test users and creates short urls for them over HTTP, then writes the result to a JSON
// file the k6 scenarios read. It talks to the running API only, so it works against any environment.
//
//	go run ./loadtest/seed                                  # defaults: http://127.0.0.1:8085, 5 users, 20 urls each
//	BASE_URL=http://staging USERS=50 URLS_PER_USER=200 go run ./loadtest/seed
//
// loadtest/seed/seed.js does the same from k6, for when only the k6 image is available.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	apiPath  = "/api/v1/fupi"
	password = "Loadtest-Passw0rd!"
)

type user struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type shortURL struct {
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
	Owner       string `json:"owner"`
}

type seedFile struct {
	BaseURL string     `json:"base_url"`
	Users   []user     `json:"users"`
	URLs    []shortURL `json:"urls"`
}

type client struct {
	base string
	http *http.Client
}

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	base := strings.TrimRight(env("BASE_URL", "http://127.0.0.1:8085"), "/")
	users, err := strconv.Atoi(env("USERS", "5"))
	if err != nil || users < 1 {
		return fmt.Errorf("USERS must be a positive integer")
	}
	perUser, err := strconv.Atoi(env("URLS_PER_USER", "20"))
	if err != nil || perUser < 1 {
		return fmt.Errorf("URLS_PER_USER must be a positive integer")
	}
	out := env("OUT", "loadtest/.data/seed.json")

	c := &client{base: base, http: &http.Client{Timeout: 30 * time.Second}}
	runID := strconv.FormatInt(time.Now().Unix(), 36)
	expires := time.Now().AddDate(0, 0, 30).UTC().Format(time.RFC3339)
	result := seedFile{BaseURL: base}

	for i := 0; i < users; i++ {
		u := user{Email: fmt.Sprintf("lt-%s-%d@fupi.test", runID, i), Password: password}
		reg := map[string]string{"username": fmt.Sprintf("lt%s%d", runID, i), "first_name": "Load", "email": u.Email, "password": u.Password}
		if _, err := c.post("/auth/register", "", reg, http.StatusCreated); err != nil {
			return fmt.Errorf("register %s: %w", u.Email, err)
		}
		var login struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		body, err := c.post("/auth/login", "", map[string]string{"email": u.Email, "password": u.Password}, http.StatusOK)
		if err != nil {
			return fmt.Errorf("login %s: %w", u.Email, err)
		}
		if err := json.Unmarshal(body, &login); err != nil {
			return fmt.Errorf("decode login response: %w", err)
		}
		u.AccessToken, u.RefreshToken = login.AccessToken, login.RefreshToken
		result.Users = append(result.Users, u)

		for j := 0; j < perUser; j++ {
			target := fmt.Sprintf("https://example.com/loadtest/%s/%d/%d", runID, i, j)
			payload := map[string]any{"original_url": target, "expires_on": expires}
			body, err := c.post("/urls", u.AccessToken, payload, http.StatusCreated)
			if err != nil {
				return fmt.Errorf("create url %d for %s: %w", j, u.Email, err)
			}
			var created struct {
				ShortCode string `json:"short_code"`
			}
			if err := json.Unmarshal(body, &created); err != nil {
				return fmt.Errorf("decode create response: %w", err)
			}
			result.URLs = append(result.URLs, shortURL{ShortCode: created.ShortCode, OriginalURL: target, Owner: u.Email})
		}
		slog.Info("seeded user", "email", u.Email, "urls", perUser)
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return err
	}
	slog.Info("wrote seed file", "path", out, "users", len(result.Users), "urls", len(result.URLs))
	return nil
}

// post sends a JSON body and returns the response body when the status is the expected one. A 429 is waited out for
// the time the server asks for, so a rate limiter on the API slows the seed down instead of failing it.
func (c *client) post(path, token string, payload any, want int) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequest(http.MethodPost, c.base+apiPath+path, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if wait < 1 {
				wait = 1
			}
			slog.Warn("rate limited, waiting", "seconds", wait)
			time.Sleep(time.Duration(wait) * time.Second)
			continue
		}
		if resp.StatusCode != want {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return body, nil
	}
	return nil, fmt.Errorf("still rate limited after 5 attempts")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
