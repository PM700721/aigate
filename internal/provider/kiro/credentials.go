package kiro

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// AuthType determines which refresh endpoint to use.
type AuthType int

const (
	AuthKiroDesktop AuthType = iota // Kiro IDE credentials
	AuthAWSSSOOIDC                  // AWS SSO OIDC (kiro-cli)
)

// Credentials holds all auth state.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ProfileARN   string
	Region       string

	// AWS SSO OIDC specific
	ClientID     string
	ClientSecret string
	SSORegion    string // may differ from API region
	Scopes       []string

	// Enterprise Kiro IDE
	ClientIDHash string

	AuthType AuthType

	// Source tracking for save-back
	sourceFile     string // JSON file path
	sqliteDB       string // SQLite DB path
	sqliteTokenKey string // which key we loaded from
}

// sqliteTokenKeys are searched in priority order.
var sqliteTokenKeys = []string{
	"kirocli:social:token",
	"kirocli:odic:token",
	"codewhisperer:odic:token",
}

var sqliteRegistrationKeys = []string{
	"kirocli:odic:device-registration",
	"codewhisperer:odic:device-registration",
}

// LoadFromEnv loads credentials from environment variables.
func LoadFromEnv(refreshToken, profileARN, region string) *Credentials {
	return &Credentials{
		RefreshToken: refreshToken,
		ProfileARN:   profileARN,
		Region:       region,
		AuthType:     AuthKiroDesktop,
	}
}

// LoadFromJSON loads credentials from a Kiro IDE JSON file.
func LoadFromJSON(path string) (*Credentials, error) {
	path = expandPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}

	var raw struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    string `json:"expiresAt"`
		ProfileARN   string `json:"profileArn"`
		Region       string `json:"region"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		ClientIDHash string `json:"clientIdHash"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	c := &Credentials{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		ProfileARN:   raw.ProfileARN,
		Region:       raw.Region,
		ClientID:     raw.ClientID,
		ClientSecret: raw.ClientSecret,
		ClientIDHash: raw.ClientIDHash,
		sourceFile:   path,
	}

	if raw.ExpiresAt != "" {
		c.ExpiresAt = parseTime(raw.ExpiresAt)
	}

	// Enterprise Kiro IDE: load device registration from separate file
	if raw.ClientIDHash != "" {
		c.loadEnterpriseDeviceRegistration(raw.ClientIDHash)
	}

	c.detectAuthType()
	return c, nil
}

// LoadFromSQLite loads credentials from kiro-cli SQLite database.
func LoadFromSQLite(dbPath string) (*Credentials, error) {
	dbPath = expandPath(dbPath)
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	c := &Credentials{sqliteDB: dbPath}

	// Load token (try keys in priority order)
	for _, key := range sqliteTokenKeys {
		var val string
		err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&val)
		if err != nil {
			continue
		}
		var tok struct {
			AccessToken  string   `json:"access_token"`
			RefreshToken string   `json:"refresh_token"`
			ExpiresAt    string   `json:"expires_at"`
			ProfileARN   string   `json:"profile_arn"`
			Region       string   `json:"region"`
			Scopes       []string `json:"scopes"`
		}
		if err := json.Unmarshal([]byte(val), &tok); err != nil {
			continue
		}
		c.AccessToken = tok.AccessToken
		c.RefreshToken = tok.RefreshToken
		c.ProfileARN = tok.ProfileARN
		c.SSORegion = tok.Region
		c.Scopes = tok.Scopes
		c.sqliteTokenKey = key
		if tok.ExpiresAt != "" {
			c.ExpiresAt = parseTime(tok.ExpiresAt)
		}
		break
	}

	// Load device registration (client_id, client_secret)
	for _, key := range sqliteRegistrationKeys {
		var val string
		err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&val)
		if err != nil {
			continue
		}
		var reg struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Region       string `json:"region"`
		}
		if err := json.Unmarshal([]byte(val), &reg); err != nil {
			continue
		}
		c.ClientID = reg.ClientID
		c.ClientSecret = reg.ClientSecret
		if c.SSORegion == "" {
			c.SSORegion = reg.Region
		}
		break
	}

	if c.RefreshToken == "" {
		return nil, fmt.Errorf("no credentials found in sqlite db")
	}

	c.detectAuthType()
	return c, nil
}

// Save persists updated tokens back to the original source.
func (c *Credentials) Save() {
	if c.sqliteDB != "" {
		c.saveToSQLite()
	} else if c.sourceFile != "" {
		c.saveToJSON()
	}
}

func (c *Credentials) detectAuthType() {
	if c.ClientID != "" && c.ClientSecret != "" {
		c.AuthType = AuthAWSSSOOIDC
	} else {
		c.AuthType = AuthKiroDesktop
	}
}

func (c *Credentials) loadEnterpriseDeviceRegistration(clientIDHash string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".aws", "sso", "cache", clientIDHash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var reg struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return
	}
	c.ClientID = reg.ClientID
	c.ClientSecret = reg.ClientSecret
}

func (c *Credentials) saveToJSON() {
	data, err := os.ReadFile(c.sourceFile)
	if err != nil {
		log.Printf("[auth] failed to read file for save: %v", err)
		return
	}
	var existing map[string]any
	if err := json.Unmarshal(data, &existing); err != nil {
		existing = make(map[string]any)
	}
	existing["accessToken"] = c.AccessToken
	existing["refreshToken"] = c.RefreshToken
	if !c.ExpiresAt.IsZero() {
		existing["expiresAt"] = c.ExpiresAt.Format(time.RFC3339)
	}
	out, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(c.sourceFile, out, 0600); err != nil {
		log.Printf("[auth] failed to save credentials: %v", err)
	}
}

func (c *Credentials) saveToSQLite() {
	db, err := sql.Open("sqlite3", c.sqliteDB)
	if err != nil {
		log.Printf("[auth] failed to open sqlite for save: %v", err)
		return
	}
	defer db.Close()

	tok := map[string]any{
		"access_token":  c.AccessToken,
		"refresh_token": c.RefreshToken,
		"region":        c.SSORegion,
	}
	if !c.ExpiresAt.IsZero() {
		tok["expires_at"] = c.ExpiresAt.Format(time.RFC3339)
	}
	if len(c.Scopes) > 0 {
		tok["scopes"] = c.Scopes
	}
	val, _ := json.Marshal(tok)

	key := c.sqliteTokenKey
	if key == "" {
		key = sqliteTokenKeys[0]
	}
	_, err = db.Exec("UPDATE auth_kv SET value = ? WHERE key = ?", string(val), key)
	if err != nil {
		log.Printf("[auth] failed to save to sqlite: %v", err)
	}
}

// IsExpiringSoon returns true if token expires within 10 minutes.
func (c *Credentials) IsExpiringSoon() bool {
	if c.ExpiresAt.IsZero() {
		return true
	}
	return time.Until(c.ExpiresAt) < 10*time.Minute
}

// IsExpired returns true if token has already expired.
func (c *Credentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return true
	}
	return time.Now().After(c.ExpiresAt)
}

// --- helpers ---

func expandPath(p string) string {
	if len(p) > 0 && p[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[1:])
	}
	return p
}

func parseTime(s string) time.Time {
	// Try RFC3339 first, then ISO 8601 variants
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
