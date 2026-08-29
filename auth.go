package sendafrica

import (
	"context"
	"net/http"
)

// Auth models and methods for registration, login, JWT refresh, OTP, OAuth,
// profile management, and API-key rotation. Most of these are public or use
// the bearer-token (JWT) authentication mode.

type RegisterRequest struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	CompanyName string `json:"company_name,omitempty"`
	Phone       string `json:"phone,omitempty"`
}

type RegistrationResult struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Message   string `json:"message"`
}

// Register creates a new account. It normalizes an optional phone and sends an
// OTP email automatically. Public endpoint.
func (c *Client) Register(ctx context.Context, request RegisterRequest) (RegistrationResult, error) {
	if request.Phone != "" {
		normalized, err := NormalizeTZPhone(request.Phone)
		if err != nil {
			return RegistrationResult{}, err
		}
		request.Phone = normalized
	}
	var out RegistrationResult
	_, err := c.do(ctx, http.MethodPost, "/auth/register", nil, request, &out, RequestOptions{})
	return out, err
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResult struct {
	AccessToken     string `json:"access_token"`
	RefreshToken    string `json:"refresh_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	EmailVerified   bool   `json:"email_verified"`
	PhoneVerified   bool   `json:"phone_verified"`
	ProfileComplete bool   `json:"profile_complete"`
}

// Login verifies credentials and returns a short-lived JWT pair. Public
// endpoint. The access token is typically bound to the client with
// WithBearerToken for subsequent calls.
func (c *Client) Login(ctx context.Context, email, password string) (LoginResult, error) {
	var out LoginResult
	_, err := c.do(ctx, http.MethodPost, "/auth/login", nil, LoginRequest{Email: email, Password: password}, &out, RequestOptions{})
	return out, err
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// Refresh rotates a refresh token into a new access token. Public endpoint.
// The returned refresh token is the new one and must replace the old value.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	var out TokenPair
	_, err := c.do(ctx, http.MethodPost, "/auth/refresh", nil, RefreshRequest{RefreshToken: refreshToken}, &out, RequestOptions{})
	return out, err
}

type VerifyEmailRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

type VerifyEmailResult struct {
	Message       string `json:"message"`
	EmailVerified bool   `json:"email_verified"`
	CanLogin      bool   `json:"can_login"`
}

// SendVerificationEmail triggers an email OTP. It always returns 200 whether
// or not the address exists to prevent email enumeration. Public endpoint.
func (c *Client) SendVerificationEmail(ctx context.Context, email string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/send-verification-email", nil, map[string]string{"email": email}, nil, RequestOptions{})
	return err
}

// VerifyEmail confirms an email OTP. Public endpoint.
func (c *Client) VerifyEmail(ctx context.Context, email, otp string) (VerifyEmailResult, error) {
	var out VerifyEmailResult
	_, err := c.do(ctx, http.MethodPost, "/auth/verify-email", nil, VerifyEmailRequest{Email: email, OTP: otp}, &out, RequestOptions{})
	return out, err
}

// ResetPassword requests a password-reset OTP sent by email. Public endpoint.
func (c *Client) ResetPassword(ctx context.Context, email string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/reset-password", nil, map[string]string{"email": email}, nil, RequestOptions{})
	return err
}

type ResetPasswordConfirmRequest struct {
	Email       string `json:"email"`
	OTP         string `json:"otp"`
	NewPassword string `json:"new_password"`
}

// ResetPasswordConfirm confirms a reset with the OTP and a new password.
// Public endpoint.
func (c *Client) ResetPasswordConfirm(ctx context.Context, email, otp, newPassword string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/reset-password-confirm", nil, ResetPasswordConfirmRequest{Email: email, OTP: otp, NewPassword: newPassword}, nil, RequestOptions{})
	return err
}

// OAuthExchange redeems a single-use exchange code (from a provider redirect)
// for a JWT pair. Public endpoint.
func (c *Client) OAuthExchange(ctx context.Context, exchangeCode string) (TokenPair, error) {
	var out TokenPair
	_, err := c.do(ctx, http.MethodPost, "/auth/oauth/exchange", nil, map[string]string{"exchange_code": exchangeCode}, &out, RequestOptions{})
	return out, err
}

type UserProfile struct {
	ID            string `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	CompanyName   string `json:"company_name"`
	EmailVerified bool   `json:"email_verified"`
	PhoneVerified bool   `json:"phone_verified"`
}

// Me returns the current user profile. Requires a credential.
func (c *Client) Me(ctx context.Context) (UserProfile, error) {
	if err := c.requireAuth(); err != nil {
		return UserProfile{}, err
	}
	var out UserProfile
	_, err := c.do(ctx, http.MethodGet, "/auth/me", nil, nil, &out, RequestOptions{})
	return out, err
}

type UpdateProfileRequest struct {
	FirstName   string `json:"first_name,omitempty"`
	LastName    string `json:"last_name,omitempty"`
	Phone       string `json:"phone,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
}

// UpdateMe updates the current profile and/or phone. Requires a credential.
func (c *Client) UpdateMe(ctx context.Context, request UpdateProfileRequest) (UserProfile, error) {
	if err := c.requireAuth(); err != nil {
		return UserProfile{}, err
	}
	if request.Phone != "" {
		normalized, err := NormalizeTZPhone(request.Phone)
		if err != nil {
			return UserProfile{}, err
		}
		request.Phone = normalized
	}
	var out UserProfile
	_, err := c.do(ctx, http.MethodPut, "/auth/me", nil, request, &out, RequestOptions{})
	return out, err
}

// Logout blacklists the current access token and revokes its refresh token.
// Requires a credential.
func (c *Client) Logout(ctx context.Context) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/auth/logout", nil, nil, nil, RequestOptions{})
	return err
}

// ChangePassword changes the account password and revokes other sessions.
// Requires a credential.
func (c *Client) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/auth/change-password", nil, map[string]string{
		"current_password": currentPassword,
		"new_password":     newPassword,
	}, nil, RequestOptions{})
	return err
}

// SendPhoneOTP triggers phone verification via SMS. Requires a credential.
func (c *Client) SendPhoneOTP(ctx context.Context) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/auth/send-phone-otp", nil, nil, nil, RequestOptions{})
	return err
}

// VerifyPhone confirms a phone OTP. Requires a credential.
func (c *Client) VerifyPhone(ctx context.Context, otp string) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, "/auth/verify-phone", nil, map[string]string{"otp": otp}, nil, RequestOptions{})
	return err
}

type APIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key,omitempty"`
	CreatedAt string `json:"created_at"`
}

// ListAPIKeys lists API-key metadata. The raw key is never returned.
// Requires a credential.
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	if err := c.requireAuth(); err != nil {
		return nil, err
	}
	var out []APIKey
	_, err := c.do(ctx, http.MethodGet, "/auth/api-keys", nil, nil, &out, RequestOptions{})
	return out, err
}

// CreateAPIKey creates a new API key. The Key field is populated exactly once
// and cannot be retrieved again. Requires a credential.
func (c *Client) CreateAPIKey(ctx context.Context, name string) (APIKey, error) {
	if err := c.requireAuth(); err != nil {
		return APIKey{}, err
	}
	var out APIKey
	_, err := c.do(ctx, http.MethodPost, "/auth/api-keys", nil, map[string]string{"name": name}, &out, RequestOptions{})
	return out, err
}

// DeleteAPIKey revokes an API key immediately. Requires a credential.
func (c *Client) DeleteAPIKey(ctx context.Context, keyID string) error {
	if err := c.requireAuth(); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodDelete, "/auth/api-keys/"+keyID, nil, nil, nil, RequestOptions{})
	return err
}
