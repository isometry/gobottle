package oci

import (
	"github.com/google/go-containerregistry/pkg/authn"
)

// Authenticator handles GHCR authentication
type Authenticator struct {
	host  string
	token string
}

// NewAuthenticator creates an authenticator from token
func NewAuthenticator(host, token string) *Authenticator {
	return &Authenticator{
		host:  host,
		token: token,
	}
}

// GetAuthConfig returns the authn.AuthConfig for go-containerregistry
func (a *Authenticator) GetAuthConfig() authn.AuthConfig {
	return authn.AuthConfig{
		Username: "oauth2",
		Password: a.token,
	}
}

// Keychain returns an authn.Keychain for go-containerregistry
func (a *Authenticator) Keychain() authn.Keychain {
	return &authKeychain{auth: a}
}

// authKeychain implements authn.Keychain
type authKeychain struct {
	auth *Authenticator
}

// Resolve implements authn.Keychain interface
func (k *authKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	// Only provide credentials for the configured host
	if resource.RegistryStr() == k.auth.host {
		return authn.FromConfig(k.auth.GetAuthConfig()), nil
	}
	// Fall back to anonymous for other registries
	return authn.Anonymous, nil
}
