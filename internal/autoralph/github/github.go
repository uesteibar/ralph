package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
	"github.com/uesteibar/ralph/internal/autoralph/retry"

	"github.com/bradleyfalzon/ghinstallation/v2"
	jwt "github.com/golang-jwt/jwt/v4"
)

// PR represents a GitHub pull request.
type PR struct {
	Number  int
	HTMLURL string
	Title   string
	State   string
	HeadSHA string
}

// CheckRun represents a GitHub Actions check run.
type CheckRun struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	HTMLURL    string
}

// Review represents a GitHub pull request review.
type Review struct {
	ID     int64
	State  string
	Body   string
	User   string
	UserID int64
}

// Comment represents a GitHub pull request comment (review comment or issue comment).
type Comment struct {
	ID        int64
	Body      string
	Path      string
	User      string
	InReplyTo int64
}

// Client is a typed GitHub API client wrapping go-github.
type Client struct {
	gh           *gh.Client
	retryBackoff []time.Duration
}

// Option configures a Client.
type Option func(*clientConfig)

// AppCredentials holds GitHub App authentication parameters.
type AppCredentials struct {
	ClientID       string
	InstallationID int64
	PrivateKeyPath string
}

type clientConfig struct {
	baseURL      string
	retryBackoff []time.Duration
	app          *AppCredentials
}

// readKeyFile is a variable for testing; defaults to os.ReadFile.
var readKeyFile = os.ReadFile

// WithBaseURL overrides the GitHub API base URL (useful for testing).
func WithBaseURL(url string) Option {
	return func(c *clientConfig) { c.baseURL = url }
}

// WithRetryBackoff overrides the default retry backoff delays.
func WithRetryBackoff(delays ...time.Duration) Option {
	return func(c *clientConfig) { c.retryBackoff = delays }
}

// WithAppAuth configures GitHub App authentication using a Client ID,
// installation ID, and private key file. When set, token is ignored.
func WithAppAuth(app AppCredentials) Option {
	return func(c *clientConfig) { c.app = &app }
}

// New creates a new GitHub API client. When WithAppAuth is provided, the client
// authenticates as a GitHub App installation (token parameter is ignored).
// Otherwise it authenticates with the given personal access token.
func New(token string, opts ...Option) (*Client, error) {
	cfg := &clientConfig{}
	for _, o := range opts {
		o(cfg)
	}

	var client *gh.Client

	if cfg.app != nil {
		httpClient, err := newAppHTTPClient(cfg.app, cfg.baseURL)
		if err != nil {
			return nil, fmt.Errorf("configuring GitHub App auth: %w", err)
		}
		client = gh.NewClient(httpClient)
		if cfg.baseURL != "" {
			client, _ = client.WithEnterpriseURLs(cfg.baseURL, cfg.baseURL)
		}
	} else {
		client = gh.NewClient(nil).WithAuthToken(token)
		if cfg.baseURL != "" {
			client, _ = client.WithEnterpriseURLs(cfg.baseURL, cfg.baseURL)
		}
	}

	return &Client{gh: client, retryBackoff: cfg.retryBackoff}, nil
}

// newAppHTTPClient creates an http.Client with a GitHub App installation
// transport that uses Client ID (string) as the JWT issuer.
func newAppHTTPClient(app *AppCredentials, baseURL string) (*http.Client, error) {
	keyPath := expandHome(app.PrivateKeyPath)
	keyData, err := readKeyFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key %s: %w", app.PrivateKeyPath, err)
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyData)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	// Build an AppsTransport with a custom signer that uses Client ID as issuer.
	signer := &clientIDSigner{
		clientID: app.ClientID,
		method:   jwt.SigningMethodRS256,
		key:      key,
	}

	atr, err := ghinstallation.NewAppsTransportWithOptions(
		http.DefaultTransport, 0, // appID unused — our signer overrides the issuer
		ghinstallation.WithSigner(signer),
	)
	if err != nil {
		return nil, fmt.Errorf("creating apps transport: %w", err)
	}

	if baseURL != "" {
		atr.BaseURL = baseURL
	}

	itr := ghinstallation.NewFromAppsTransport(atr, app.InstallationID)
	if baseURL != "" {
		itr.BaseURL = baseURL
	}

	return &http.Client{Transport: itr}, nil
}

// clientIDSigner implements ghinstallation.Signer using a string Client ID
// as the JWT issuer instead of a numeric App ID.
type clientIDSigner struct {
	clientID string
	method   jwt.SigningMethod
	key      any
}

func (s *clientIDSigner) Sign(claims jwt.Claims) (string, error) {
	// Override the issuer with our Client ID.
	if rc, ok := claims.(*jwt.RegisteredClaims); ok {
		rc.Issuer = s.clientID
	}
	return jwt.NewWithClaims(s.method, claims).SignedString(s.key)
}

// CreatePullRequest creates a new pull request and returns it.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo, head, base, title, body string) (PR, error) {
	return retry.DoVal(ctx, func() (PR, error) {
		pr, _, err := c.gh.PullRequests.Create(ctx, owner, repo, &gh.NewPullRequest{
			Title: gh.Ptr(title),
			Head:  gh.Ptr(head),
			Base:  gh.Ptr(base),
			Body:  gh.Ptr(body),
		})
		if err != nil {
			return PR{}, classifyErr(fmt.Errorf("creating pull request: %w", err))
		}
		return prFromGH(pr), nil
	}, c.retryOpts()...)
}

// FetchPRReviews returns all reviews on the given pull request.
func (c *Client) FetchPRReviews(ctx context.Context, owner, repo string, prNumber int) ([]Review, error) {
	return retry.DoVal(ctx, func() ([]Review, error) {
		var all []Review
		opts := &gh.ListOptions{PerPage: 100}
		for {
			reviews, resp, err := c.gh.PullRequests.ListReviews(ctx, owner, repo, prNumber, opts)
			if err != nil {
				return nil, classifyErr(fmt.Errorf("fetching PR reviews: %w", err))
			}
			for _, r := range reviews {
				all = append(all, reviewFromGH(r))
			}
			if resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
		return all, nil
	}, c.retryOpts()...)
}

// FetchPRComments returns all review comments on the given pull request.
func (c *Client) FetchPRComments(ctx context.Context, owner, repo string, prNumber int) ([]Comment, error) {
	return retry.DoVal(ctx, func() ([]Comment, error) {
		var all []Comment
		opts := &gh.PullRequestListCommentsOptions{
			ListOptions: gh.ListOptions{PerPage: 100},
		}
		for {
			comments, resp, err := c.gh.PullRequests.ListComments(ctx, owner, repo, prNumber, opts)
			if err != nil {
				return nil, classifyErr(fmt.Errorf("fetching PR comments: %w", err))
			}
			for _, cm := range comments {
				all = append(all, reviewCommentFromGH(cm))
			}
			if resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
		return all, nil
	}, c.retryOpts()...)
}

// PostPRComment posts a general comment on the pull request (issue comment).
func (c *Client) PostPRComment(ctx context.Context, owner, repo string, prNumber int, body string) (Comment, error) {
	return retry.DoVal(ctx, func() (Comment, error) {
		ic, _, err := c.gh.Issues.CreateComment(ctx, owner, repo, prNumber, &gh.IssueComment{
			Body: gh.Ptr(body),
		})
		if err != nil {
			return Comment{}, classifyErr(fmt.Errorf("posting PR comment: %w", err))
		}
		return Comment{
			ID:   ic.GetID(),
			Body: ic.GetBody(),
			User: ic.GetUser().GetLogin(),
		}, nil
	}, c.retryOpts()...)
}

// PostReviewReply replies to a specific review comment on the pull request.
func (c *Client) PostReviewReply(ctx context.Context, owner, repo string, prNumber int, commentID int64, body string) (Comment, error) {
	return retry.DoVal(ctx, func() (Comment, error) {
		cm, _, err := c.gh.PullRequests.CreateCommentInReplyTo(ctx, owner, repo, prNumber, body, commentID)
		if err != nil {
			return Comment{}, classifyErr(fmt.Errorf("posting review reply: %w", err))
		}
		return reviewCommentFromGH(cm), nil
	}, c.retryOpts()...)
}

// IsPRMerged returns whether the given pull request has been merged.
func (c *Client) IsPRMerged(ctx context.Context, owner, repo string, prNumber int) (bool, error) {
	return retry.DoVal(ctx, func() (bool, error) {
		merged, _, err := c.gh.PullRequests.IsMerged(ctx, owner, repo, prNumber)
		if err != nil {
			return false, classifyErr(fmt.Errorf("checking PR merged status: %w", err))
		}
		return merged, nil
	}, c.retryOpts()...)
}

func prFromGH(pr *gh.PullRequest) PR {
	p := PR{
		Number:  pr.GetNumber(),
		HTMLURL: pr.GetHTMLURL(),
		Title:   pr.GetTitle(),
		State:   pr.GetState(),
	}
	if pr.Head != nil {
		p.HeadSHA = pr.Head.GetSHA()
	}
	return p
}

func reviewFromGH(r *gh.PullRequestReview) Review {
	return Review{
		ID:     r.GetID(),
		State:  r.GetState(),
		Body:   r.GetBody(),
		User:   r.GetUser().GetLogin(),
		UserID: r.GetUser().GetID(),
	}
}

func reviewCommentFromGH(cm *gh.PullRequestComment) Comment {
	return Comment{
		ID:        cm.GetID(),
		Body:      cm.GetBody(),
		Path:      cm.GetPath(),
		User:      cm.GetUser().GetLogin(),
		InReplyTo: cm.GetInReplyTo(),
	}
}

// retryOpts returns the retry options for this client.
func (c *Client) retryOpts() []retry.Option {
	if len(c.retryBackoff) > 0 {
		return []retry.Option{retry.WithBackoff(c.retryBackoff...)}
	}
	return nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// classifyErr wraps a go-github error as permanent if it's a client error (4xx),
// and leaves it retryable for server errors (5xx) and network errors.
func classifyErr(err error) error {
	if err == nil {
		return nil
	}
	var ghErr *gh.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		if ghErr.Response.StatusCode >= 400 && ghErr.Response.StatusCode < 500 {
			return retry.Permanent(err)
		}
	}
	return err
}

// FetchCheckRuns returns all check runs for the given git ref (SHA, branch, or tag).
func (c *Client) FetchCheckRuns(ctx context.Context, owner, repo, ref string) ([]CheckRun, error) {
	return retry.DoVal(ctx, func() ([]CheckRun, error) {
		var all []CheckRun
		opts := &gh.ListCheckRunsOptions{
			ListOptions: gh.ListOptions{PerPage: 100},
		}
		for {
			result, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
			if err != nil {
				return nil, classifyErr(fmt.Errorf("fetching check runs: %w", err))
			}
			for _, cr := range result.CheckRuns {
				all = append(all, CheckRun{
					ID:         cr.GetID(),
					Name:       cr.GetName(),
					Status:     cr.GetStatus(),
					Conclusion: cr.GetConclusion(),
					HTMLURL:    cr.GetHTMLURL(),
				})
			}
			if resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
		return all, nil
	}, c.retryOpts()...)
}

// FetchCheckRunLog downloads the log for a check run. The GitHub API returns a
// redirect to a download URL. Returns empty bytes with no error if the log is
// unavailable.
func (c *Client) FetchCheckRunLog(ctx context.Context, owner, repo string, checkRunID int64) ([]byte, error) {
	return retry.DoVal(ctx, func() ([]byte, error) {
		u := fmt.Sprintf("repos/%v/%v/actions/jobs/%v/logs", owner, repo, checkRunID)
		req, err := c.gh.NewRequest("GET", u, nil)
		if err != nil {
			return nil, classifyErr(fmt.Errorf("creating check run log request: %w", err))
		}

		resp, err := c.gh.BareDo(ctx, req)
		if err != nil {
			// BareDo treats redirects as errors. Follow the redirect to download.
			if resp != nil && (resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently) {
				loc := resp.Header.Get("Location")
				resp.Body.Close()
				if loc != "" {
					return c.downloadURL(ctx, loc)
				}
			}
			if resp != nil {
				resp.Body.Close()
			}
			// 404 or other client errors mean logs are unavailable.
			var ghErr *gh.ErrorResponse
			if errors.As(err, &ghErr) && ghErr.Response != nil &&
				ghErr.Response.StatusCode >= 400 && ghErr.Response.StatusCode < 500 {
				return nil, nil
			}
			return nil, classifyErr(fmt.Errorf("fetching check run log: %w", err))
		}
		defer resp.Body.Close()

		// If we got a 200 response directly (unlikely but handle it).
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading check run log body: %w", err)
		}
		return body, nil
	}, c.retryOpts()...)
}

// downloadURL fetches the content at the given URL.
func (c *Client) downloadURL(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading log: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading download body: %w", err)
	}
	return body, nil
}

// FetchPR fetches a single pull request by number.
func (c *Client) FetchPR(ctx context.Context, owner, repo string, prNumber int) (PR, error) {
	return retry.DoVal(ctx, func() (PR, error) {
		pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, prNumber)
		if err != nil {
			return PR{}, classifyErr(fmt.Errorf("fetching pull request: %w", err))
		}
		return prFromGH(pr), nil
	}, c.retryOpts()...)
}

// FindOpenPR finds an existing open PR for the given head and base branches.
// Returns the PR if found, or nil if no matching open PR exists.
func (c *Client) FindOpenPR(ctx context.Context, owner, repo, head, base string) (*PR, error) {
	result, err := retry.DoVal(ctx, func() (*PR, error) {
		prs, _, err := c.gh.PullRequests.List(ctx, owner, repo, &gh.PullRequestListOptions{
			Head:  owner + ":" + head,
			Base:  base,
			State: "open",
		})
		if err != nil {
			return nil, classifyErr(fmt.Errorf("listing PRs: %w", err))
		}
		if len(prs) == 0 {
			return nil, nil
		}
		pr := prFromGH(prs[0])
		return &pr, nil
	}, c.retryOpts()...)
	return result, err
}
