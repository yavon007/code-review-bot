package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "reviewbot_session"

type sessionPayload struct {
	UserID         int64 `json:"user_id"`
	Exp            int64 `json:"exp"`
	SessionVersion int   `json:"session_version"`
}

func setSessionCookie(w http.ResponseWriter, secret string, userID int64, sessionVersion int) {
	expires := time.Now().Add(7 * 24 * time.Hour)
	payload := sessionPayload{UserID: userID, Exp: expires.Unix(), SessionVersion: sessionVersion}
	encoded := encodeSession(secret, payload)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    encoded,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func readSession(r *http.Request, secret string) (sessionPayload, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return sessionPayload{}, false
	}
	payload, ok := decodeSession(secret, cookie.Value)
	if !ok || payload.Exp < time.Now().Unix() {
		return sessionPayload{}, false
	}
	return payload, true
}

func encodeSession(secret string, payload sessionPayload) string {
	body, _ := json.Marshal(payload)
	bodyText := base64.RawURLEncoding.EncodeToString(body)
	sig := signSession(secret, bodyText)
	return bodyText + "." + sig
}

func decodeSession(secret string, token string) (sessionPayload, bool) {
	bodyText, sig, ok := strings.Cut(token, ".")
	if !ok || sig == "" || !hmac.Equal([]byte(sig), []byte(signSession(secret, bodyText))) {
		return sessionPayload{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(bodyText)
	if err != nil {
		return sessionPayload{}, false
	}
	var payload sessionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return sessionPayload{}, false
	}
	return payload, payload.UserID > 0
}

func signSession(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sessionSecretFallback(port string) string {
	return "dev-session-secret:" + strconv.Quote(port)
}
