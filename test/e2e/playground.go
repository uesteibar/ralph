package e2e

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/server"
	mockgithub "github.com/uesteibar/ralph/test/e2e/mocks/github"
	mocklinear "github.com/uesteibar/ralph/test/e2e/mocks/linear"
)

// Playground is a self-contained E2E test environment that starts mock
// servers, a database, and the autoralph HTTP server.
type Playground struct {
	t *testing.T

	Linear *mocklinear.Mock
	GitHub *mockgithub.Mock

	DB     *db.DB
	Server *server.Server

	LinearURL string
	GitHubURL string

	Addr       string
	ProjectDir string
}

// PlaygroundConfig controls playground setup.
type PlaygroundConfig struct {
	SeedProject bool
	ProjectName string
}

func defaultPlaygroundConfig() PlaygroundConfig {
	return PlaygroundConfig{
		SeedProject: true,
		ProjectName: "test-project",
	}
}

// StartPlayground starts a complete E2E playground with mock servers,
// database, and autoralph HTTP server. It registers cleanup via t.Cleanup.
func StartPlayground(t *testing.T, opts ...func(*PlaygroundConfig)) *Playground {
	t.Helper()
	cfg := defaultPlaygroundConfig()
	for _, o := range opts {
		o(&cfg)
	}

	linearMock := mocklinear.New()
	linearSrv := linearMock.Server(t)

	githubMock := mockgithub.New()
	githubSrv := githubMock.Server(t)

	dbPath := filepath.Join(t.TempDir(), "playground.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("opening playground DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	projectDir := setupPlaygroundProject(t)

	if cfg.SeedProject {
		// Seed team and user in mock so ResolveTeamID/ResolveUserID work.
		linearMock.AddTeam(mocklinear.Team{
			ID: mocklinear.TestTeamID, Key: "TEST", Name: "Test Team",
		})
		linearMock.AddUser(mocklinear.User{
			ID: mocklinear.TestAssigneeID, Name: "Test Assignee",
			DisplayName: "test-assignee", Email: "test@test.com",
		})

		_, err := database.CreateProject(db.Project{
			Name:               cfg.ProjectName,
			LocalPath:          projectDir,
			CredentialsProfile: "",
			GithubOwner:        "test-owner",
			GithubRepo:         "test-repo",
			LinearTeamID:       mocklinear.TestTeamID,
			LinearAssigneeID:   mocklinear.TestAssigneeID,
			RalphConfigPath:    ".ralph/ralph.yaml",
			MaxIterations:      20,
			BranchPrefix:       "autoralph/",
		})
		if err != nil {
			t.Fatalf("seeding project: %v", err)
		}
	}

	srvCfg := server.Config{DB: database}
	srv, err := server.New("127.0.0.1:0", srvCfg)
	if err != nil {
		t.Fatalf("starting playground server: %v", err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })

	pg := &Playground{
		t:          t,
		Linear:     linearMock,
		GitHub:     githubMock,
		DB:         database,
		Server:     srv,
		LinearURL:  linearSrv.URL,
		GitHubURL:  githubSrv.URL,
		Addr:       srv.Addr(),
		ProjectDir: projectDir,
	}

	pg.waitForHealth()
	return pg
}

// SeedIssue creates an issue in the database with the given state.
func (pg *Playground) SeedIssue(identifier, title, state string) db.Issue {
	pg.t.Helper()

	projects, err := pg.DB.ListProjects()
	if err != nil || len(projects) == 0 {
		pg.t.Fatal("no projects in DB, cannot seed issue")
	}

	issue, err := pg.DB.CreateIssue(db.Issue{
		ProjectID:     projects[0].ID,
		LinearIssueID: mocklinear.IssueUUID(identifier),
		Identifier:    identifier,
		Title:         title,
		State:         state,
	})
	if err != nil {
		pg.t.Fatalf("seeding issue: %v", err)
	}
	return issue
}

// BaseURL returns the full base URL of the running server.
func (pg *Playground) BaseURL() string {
	return "http://" + pg.Addr
}

func (pg *Playground) waitForHealth() {
	pg.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(pg.BaseURL() + "/api/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	pg.t.Fatal("playground server did not become healthy within 5s")
}

// setupPlaygroundProject copies the E2E test-project fixture to a temp dir
// and initializes it as a git repository.
func setupPlaygroundProject(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "test-project")

	fixtureDir := filepath.Join("fixtures", "test-project")
	if err := cpDir(fixtureDir, projectDir); err != nil {
		t.Fatalf("copying test-project fixture: %v", err)
	}

	gitRun(t, projectDir, "init")
	gitRun(t, projectDir, "add", "-A")
	gitRun(t, projectDir, "commit", "-m", "initial commit")

	bareDir := filepath.Join(tmpDir, "remote.git")
	cmd := exec.Command("git", "init", "--bare", bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creating bare remote: %v\n%s", err, out)
	}

	gitRun(t, projectDir, "remote", "add", "origin", bareDir)
	gitRun(t, projectDir, "push", "-u", "origin", "main")

	return projectDir
}

// gitRun executes a git command in the given directory.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// cpDir recursively copies src to dst.
func cpDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
