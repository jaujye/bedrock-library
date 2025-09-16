package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"patyhank_gomc/config"

	"github.com/sandertv/gophertunnel/minecraft/auth"
	"golang.org/x/oauth2"
)

// TokenCache represents the structure for storing authentication tokens
type TokenCache struct {
	Token *oauth2.Token `json:"token,omitempty"`
}

// GetToken retrieves a valid authentication token
// Uses cached token if available and valid, otherwise requests new authentication
func GetToken(cfg *config.Config) (*oauth2.Token, error) {
	cacheFile := cfg.Auth.TokenCacheFile

	// Try to load cached token first
	if token, err := loadCachedToken(cacheFile); err == nil && token.Valid() {
		fmt.Println("Using cached authentication token")
		return token, nil
	}

	// Request new token via device flow
	fmt.Println("Requesting new authentication token...")
	token, err := auth.RequestLiveToken()
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	// Save token to cache
	if err := saveCachedToken(cacheFile, token); err != nil {
		fmt.Printf("Warning: Failed to cache token: %v\n", err)
	}

	return token, nil
}

// loadCachedToken loads authentication token from cache file
func loadCachedToken(cacheFile string) (*oauth2.Token, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}

	var cache TokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	if cache.Token == nil {
		return nil, fmt.Errorf("no token found in cache")
	}

	return cache.Token, nil
}

// saveCachedToken saves authentication token to cache file
func saveCachedToken(cacheFile string, token *oauth2.Token) error {
	// Ensure directory exists
	dir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	cache := TokenCache{
		Token: token,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFile, data, 0600)
}

// ClearCache removes the cached authentication token
func ClearCache(cfg *config.Config) error {
	cacheFile := cfg.Auth.TokenCacheFile
	if err := os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Authentication cache cleared")
	return nil
}