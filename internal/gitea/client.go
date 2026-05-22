package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

type CommitStatus struct {
	State       string `json:"state"`
	TargetURL   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

type IssueComment struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
}

type ChangedFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
}

func NewClient(baseURL string, token string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL: parsed,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (c *Client) ListPullRequestFiles(ctx context.Context, owner string, repo string, prNumber int) ([]ChangedFile, error) {
	endpoint := c.apiPath("repos", owner, repo, "pulls", strconv.Itoa(prNumber), "files")
	var files []ChangedFile
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &files); err != nil {
		return nil, err
	}
	return files, nil
}

func (c *Client) GetPullRequestDiff(ctx context.Context, owner string, repo string, prNumber int, maxBytes int64) (string, bool, error) {
	endpoint := c.apiPath("repos", owner, repo, "pulls", strconv.Itoa(prNumber)) + ".diff"
	body, truncated, err := c.doRaw(ctx, http.MethodGet, endpoint, maxBytes)
	if err != nil {
		return "", false, err
	}
	return string(body), truncated, nil
}

func (c *Client) CreateCommitStatus(ctx context.Context, owner string, repo string, sha string, status CommitStatus) error {
	endpoint := c.apiPath("repos", owner, repo, "statuses", sha)
	return c.doJSON(ctx, http.MethodPost, endpoint, status, nil)
}

func (c *Client) CreateIssueComment(ctx context.Context, owner string, repo string, prNumber int, body string) (IssueComment, error) {
	endpoint := c.apiPath("repos", owner, repo, "issues", strconv.Itoa(prNumber), "comments")
	payload := map[string]string{"body": body}
	var comment IssueComment
	if err := c.doJSON(ctx, http.MethodPost, endpoint, payload, &comment); err != nil {
		return IssueComment{}, err
	}
	return comment, nil
}

func (c *Client) apiPath(parts ...string) string {
	escaped := make([]string, 0, len(parts)+1)
	escaped = append(escaped, "api", "v1")
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return path.Join(escaped...)
}

func (c *Client) doJSON(ctx context.Context, method string, endpoint string, input any, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := c.newRequest(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("gitea api returned status %d", res.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(output)
}

func (c *Client) doRaw(ctx context.Context, method string, endpoint string, maxBytes int64) ([]byte, bool, error) {
	req, err := c.newRequest(ctx, method, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "text/plain")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, false, fmt.Errorf("gitea api returned status %d", res.StatusCode)
	}
	if maxBytes <= 0 {
		body, err := io.ReadAll(res.Body)
		return body, false, err
	}
	limited := io.LimitReader(res.Body, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > maxBytes {
		return body[:maxBytes], true, nil
	}
	return body, false, nil
}

func (c *Client) newRequest(ctx context.Context, method string, endpoint string, body io.Reader) (*http.Request, error) {
	requestURL := *c.baseURL
	requestURL.Path = path.Join(requestURL.Path, endpoint)

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	return req, nil
}
