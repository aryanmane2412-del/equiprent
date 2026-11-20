package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/httpx"
)

// Handler exposes the auth endpoints.
type Handler struct {
	svc *Service
	// limit is the per-IP rate limit for the public auth endpoints (may be nil).
	limit gin.HandlerFunc
}

// NewHandler creates the handler. authLimit may be nil.
func NewHandler(svc *Service, authLimit gin.HandlerFunc) *Handler {
	return &Handler{svc: svc, limit: authLimit}
}

func (h *Handler) Name() string { return "auth" }

func client(c *gin.Context) ClientInfo {
	return ClientInfo{Platform: c.GetHeader("X-Platform"), UserAgent: c.Request.UserAgent(), IP: c.ClientIP()}
}

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("auth")
	r.GET("/auth/capabilities", httpx.Handle(200, func(*gin.Context, *httpx.Principal) (any, error) {
		return h.svc.Capabilities(), nil
	}), httpx.Doc{Summary: "Available sign-in methods", Response: Capabilities{},
		Description: "Mobile OTP and email links are only enabled once a delivery provider is configured (or in development simulation)."})

	pub := r
	if h.limit != nil {
		pub = r.Use(h.limit)
	}
	pub.POST("/auth/register", httpx.HandleJSON(202, func(c *gin.Context, _ *httpx.Principal, req RegisterRequest) (any, error) {
		return h.svc.Register(c.Request.Context(), req, client(c))
	}), httpx.Doc{Summary: "Create a customer, equipment owner or driver account", Request: RegisterRequest{}, Response: RegisterResponse{}, Status: 202,
		Description: "The answer is identical when the phone or email is already registered (no account enumeration). Verify the phone with /auth/otp/verify or the email with /auth/email/verify, then sign in."})
	pub.POST("/auth/login", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req LoginRequest) (any, error) {
		return h.svc.Login(c.Request.Context(), req, client(c))
	}), httpx.Doc{Summary: "Sign in with email or phone and password", Request: LoginRequest{}, Response: TokenResponse{},
		Description: "401 INVALID_CREDENTIALS for any unknown account or wrong password; 429 after repeated attempts; 403 VERIFICATION_REQUIRED if that identifier is not verified yet."})
	pub.POST("/auth/otp/request", httpx.HandleJSON(202, func(c *gin.Context, _ *httpx.Principal, req RequestOTPRequest) (any, error) {
		return h.svc.RequestOTP(c.Request.Context(), req)
	}), httpx.Doc{Summary: "Send a mobile OTP", Request: RequestOTPRequest{}, Response: RequestOTPResponse{}, Status: 202,
		Description: "503 OTP_UNAVAILABLE until an SMS provider is configured (development uses simulation)."})
	pub.POST("/auth/otp/verify", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req VerifyOTPRequest) (any, error) {
		return h.svc.VerifyOTP(c.Request.Context(), req, client(c))
	}), httpx.Doc{Summary: "Verify the OTP: proves the phone and signs in", Request: VerifyOTPRequest{}, Response: TokenResponse{}})
	pub.POST("/auth/refresh", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req RefreshRequest) (any, error) {
		return h.svc.Refresh(c.Request.Context(), req.RefreshToken, client(c))
	}), httpx.Doc{Summary: "Rotate the refresh token", Request: RefreshRequest{}, Response: TokenResponse{},
		Description: "Each refresh token works once. Reusing a rotated token revokes every session of that sign-in."})
	pub.POST("/auth/password/forgot", httpx.HandleJSON(202, func(c *gin.Context, _ *httpx.Principal, req ForgotPasswordRequest) (any, error) {
		return h.svc.ForgotPassword(c.Request.Context(), req, client(c))
	}), httpx.Doc{Summary: "Request a password reset", Request: ForgotPasswordRequest{}, Response: ForgotPasswordResponse{}, Status: 202})
	pub.POST("/auth/password/reset", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req ResetPasswordRequest) (any, error) {
		return h.svc.ResetPassword(c.Request.Context(), req, client(c))
	}), httpx.Doc{Summary: "Set a new password with the reset token or SMS code", Request: ResetPasswordRequest{}, Response: StatusResponse{},
		Description: "Signs the account out on every device."})
	pub.POST("/auth/email/verify", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req VerifyEmailRequest) (any, error) {
		return h.svc.VerifyEmail(c.Request.Context(), req, client(c))
	}), httpx.Doc{Summary: "Verify an email address", Request: VerifyEmailRequest{}, Response: StatusResponse{}})

	a := r.Authed()
	a.POST("/auth/logout", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return nil, h.svc.Logout(c.Request.Context(), p, client(c))
	}), httpx.Doc{Summary: "Sign out this device"})
	a.GET("/auth/sessions", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		list, err := h.svc.Sessions(c.Request.Context(), p)
		return SessionsResponse{Data: list}, err
	}), httpx.Doc{Summary: "List signed-in devices", Response: SessionsResponse{}})
	a.DELETE("/auth/sessions/:id", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.RevokeSession(c.Request.Context(), p, id, client(c))
	}), httpx.Doc{Summary: "Sign out another device"})
	a.POST("/auth/sessions/revoke-others", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return nil, h.svc.RevokeOtherSessions(c.Request.Context(), p, client(c))
	}), httpx.Doc{Summary: "Sign out every other device"})
	a.POST("/auth/password/change", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ChangePasswordRequest) (any, error) {
		return h.svc.ChangePassword(c.Request.Context(), p, req, client(c))
	}), httpx.Doc{Summary: "Change (or first set) your password", Request: ChangePasswordRequest{}, Response: StatusResponse{},
		Description: "Other devices are signed out."})
	a.POST("/auth/email/verification", httpx.Handle(202, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.SendEmailVerification(c.Request.Context(), p, client(c))
	}), httpx.Doc{Summary: "Send (again) the email verification link", Response: EmailVerificationResponse{}, Status: 202})
}
