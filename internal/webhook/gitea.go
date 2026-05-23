package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnsupportedEvent = errors.New("unsupported gitea event")
	ErrIgnoredAction    = errors.New("ignored gitea pull request action")
)

type GiteaPayload struct {
	Action      string      `json:"action"`
	Repository  Repository  `json:"repository"`
	PullRequest PullRequest `json:"pull_request"`
	Sender      User        `json:"sender"`
}

type Repository struct {
	FullName string `json:"full_name"`
	Name     string `json:"name"`
	Owner    User   `json:"owner"`
}

type PullRequest struct {
	Number int    `json:"number"`
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Head   PRRef  `json:"head"`
	Base   PRRef  `json:"base"`
	User   User   `json:"user"`
}

type PRRef struct {
	Ref string `json:"ref"`
	Sha string `json:"sha"`
}

type User struct {
	ID       int64  `json:"id"`
	Login    string `json:"login"`
	UserName string `json:"username"`
}

type ReviewJobInput struct {
	DeliveryID   string
	EventName    string
	Action       string
	RepoFullName string
	Owner        string
	Repo         string
	PRNumber     int
	HeadSHA      string
	BaseSHA      string
	Sender       string
}

func VerifyGiteaSignature(secret string, body []byte, signatures ...string) bool {
	if secret == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	for _, signature := range signatures {
		actual, ok := decodeSignature(signature)
		if ok && hmac.Equal(actual, expected) {
			return true
		}
	}

	return false
}

func DecodePayload(eventName string, deliveryID string, body []byte) (ReviewJobInput, error) {
	if eventName != "pull_request" && eventName != "pull_request_sync" {
		return ReviewJobInput{}, ErrUnsupportedEvent
	}

	var payload GiteaPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return ReviewJobInput{}, err
	}
	if !isReviewableAction(eventName, payload.Action) {
		return ReviewJobInput{}, ErrIgnoredAction
	}

	prNumber := payload.PullRequest.Index
	if prNumber == 0 {
		prNumber = payload.PullRequest.Number
	}
	if prNumber == 0 {
		return ReviewJobInput{}, fmt.Errorf("missing pull request number")
	}
	if payload.PullRequest.Head.Sha == "" {
		return ReviewJobInput{}, fmt.Errorf("missing pull request head sha")
	}

	owner := payload.Repository.Owner.Login
	if owner == "" {
		owner = payload.Repository.Owner.UserName
	}
	if owner == "" && payload.Repository.FullName != "" {
		parts := strings.SplitN(payload.Repository.FullName, "/", 2)
		owner = parts[0]
	}

	repo := payload.Repository.Name
	if repo == "" && payload.Repository.FullName != "" {
		parts := strings.SplitN(payload.Repository.FullName, "/", 2)
		if len(parts) == 2 {
			repo = parts[1]
		}
	}

	if owner == "" || repo == "" || payload.Repository.FullName == "" {
		return ReviewJobInput{}, fmt.Errorf("missing repository identity")
	}

	sender := payload.Sender.Login
	if sender == "" {
		sender = payload.Sender.UserName
	}

	return ReviewJobInput{
		DeliveryID:   deliveryID,
		EventName:    eventName,
		Action:       payload.Action,
		RepoFullName: payload.Repository.FullName,
		Owner:        owner,
		Repo:         repo,
		PRNumber:     prNumber,
		HeadSHA:      payload.PullRequest.Head.Sha,
		BaseSHA:      payload.PullRequest.Base.Sha,
		Sender:       sender,
	}, nil
}

func isReviewableAction(eventName string, action string) bool {
	if eventName == "pull_request_sync" {
		return true
	}
	switch action {
	case "opened", "reopened", "synchronized", "synchronize":
		return true
	default:
		return false
	}
}

func decodeSignature(signature string) ([]byte, bool) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil, false
	}
	signature = strings.TrimPrefix(signature, "sha256=")
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return nil, false
	}
	return decoded, true
}
