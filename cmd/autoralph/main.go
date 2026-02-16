package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/approve"
	"github.com/uesteibar/ralph/internal/autoralph/build"
	"github.com/uesteibar/ralph/internal/autoralph/checks"
	"github.com/uesteibar/ralph/internal/autoralph/complete"
	"github.com/uesteibar/ralph/internal/autoralph/credentials"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/internal/autoralph/feedback"
	"github.com/uesteibar/ralph/internal/autoralph/ghpoller"
	ghclient "github.com/uesteibar/ralph/internal/autoralph/github"
	"github.com/uesteibar/ralph/internal/autoralph/linear"
	"github.com/uesteibar/ralph/internal/autoralph/orchestrator"
	"github.com/uesteibar/ralph/internal/autoralph/poller"
	"github.com/uesteibar/ralph/internal/autoralph/pr"
	"github.com/uesteibar/ralph/internal/autoralph/projects"
	"github.com/uesteibar/ralph/internal/autoralph/rebase"
	"github.com/uesteibar/ralph/internal/autoralph/refine"
	"github.com/uesteibar/ralph/internal/autoralph/server"
	"github.com/uesteibar/ralph/internal/autoralph/worker"
	"github.com/uesteibar/ralph/internal/gitops"
	"github.com/uesteibar/ralph/internal/workspace"
)

var version = "dev"

const defaultAddr = "127.0.0.1:7749"

func usage() {
	fmt.Fprintf(os.Stderr, `autoralph — autonomous coding agent

Usage:
  autoralph serve [flags]   Start the HTTP server (default %s)

Flags:
  --addr         Address to listen on (default: %s)
  --dev          Proxy non-API requests to Vite dev server (localhost:5173)
  --linear-url   Override Linear API endpoint (env: AUTORALPH_LINEAR_URL)
  --github-url   Override GitHub API endpoint (env: AUTORALPH_GITHUB_URL)
`, defaultAddr, defaultAddr)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	rest := os.Args[2:]

	var err error
	switch subcmd {
	case "serve":
		err = runServe(rest)
	case "--version", "version":
		fmt.Println("autoralph " + version)
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", subcmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "autoralph %s: %v\n", subcmd, err)
		os.Exit(1)
	}
}

func runServe(args []string) error {
	addr := defaultAddr
	devMode := false
	linearURL := os.Getenv("AUTORALPH_LINEAR_URL")
	githubURL := os.Getenv("AUTORALPH_GITHUB_URL")

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 < len(args) {
				addr = args[i+1]
				i++
			}
		case "--dev":
			devMode = true
		case "--linear-url":
			if i+1 < len(args) {
				linearURL = args[i+1]
				i++
			}
		case "--github-url":
			if i+1 < len(args) {
				githubURL = args[i+1]
				i++
			}
		}
	}

	logger := slog.Default()

	// --- 1. Signal handling for graceful shutdown ---
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- 2. Open database ---
	dbPath, err := db.DefaultPath()
	if err != nil {
		return fmt.Errorf("determining database path: %w", err)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	// --- 3. Load and sync project configs ---
	configDir := credentials.DefaultPath()
	configs, warnings := projects.LoadAll(configDir)
	for _, w := range warnings {
		logger.Warn("project config", "warning", w)
	}

	if len(configs) == 0 {
		logger.Warn("no valid project configs found", "dir", configDir+"/projects/")
	}

	if err := projects.Sync(database, configs); err != nil {
		return fmt.Errorf("syncing projects: %w", err)
	}

	// --- 4. Resolve credentials and create clients per project ---
	dbProjects, err := database.ListProjects()
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}

	type gitIdentity struct {
		name  string
		email string
	}

	var (
		pollerProjects   []poller.ProjectInfo
		ghPollerProjects []ghpoller.ProjectInfo
		firstLinear      *linear.Client
		firstGitHub      *ghclient.Client
		firstGitID       *gitIdentity
	)

	for _, proj := range dbProjects {
		creds, err := credentials.Resolve(configDir, proj.CredentialsProfile)
		if err != nil {
			logger.Warn("skipping project (credentials)", "project", proj.Name, "error", err)
			continue
		}

		// Linear client
		var linearOpts []linear.Option
		if linearURL != "" {
			linearOpts = append(linearOpts, linear.WithEndpoint(linearURL))
		}
		lc := linear.New(creds.LinearAPIKey, linearOpts...)

		// GitHub client
		var ghOpts []ghclient.Option
		if githubURL != "" {
			ghOpts = append(ghOpts, ghclient.WithBaseURL(githubURL+"/"))
		}
		if creds.HasGithubApp() {
			ghOpts = append(ghOpts, ghclient.WithAppAuth(ghclient.AppCredentials{
				ClientID:       creds.GithubAppClientID,
				InstallationID: creds.GithubAppInstallationID,
				PrivateKeyPath: creds.GithubAppPrivateKeyPath,
			}))
		}
		gc, err := ghclient.New(creds.GithubToken, ghOpts...)
		if err != nil {
			logger.Warn("skipping project (github client)", "project", proj.Name, "error", err)
			continue
		}

		// Resolve team and user identifiers to UUIDs (supports names, keys, emails)
		resolvedTeamID, err := lc.ResolveTeamID(ctx, proj.LinearTeamID)
		if err != nil {
			logger.Warn("skipping project (team resolution)", "project", proj.Name, "team_id", proj.LinearTeamID, "error", err)
			continue
		}
		resolvedAssigneeID, err := lc.ResolveUserID(ctx, proj.LinearAssigneeID)
		if err != nil {
			logger.Warn("skipping project (assignee resolution)", "project", proj.Name, "assignee_id", proj.LinearAssigneeID, "error", err)
			continue
		}
		resolvedProjectID, err := lc.ResolveProjectID(ctx, proj.LinearProjectID)
		if err != nil {
			logger.Warn("skipping project (project resolution)", "project", proj.Name, "project_id", proj.LinearProjectID, "error", err)
			continue
		}

		// Persist resolved UUIDs so other components (build, complete) that
		// read the project from DB get proper UUIDs instead of names/keys.
		if resolvedTeamID != proj.LinearTeamID || resolvedAssigneeID != proj.LinearAssigneeID || resolvedProjectID != proj.LinearProjectID {
			if resolvedTeamID != proj.LinearTeamID {
				logger.Info("resolved team", "project", proj.Name, "from", proj.LinearTeamID, "to", resolvedTeamID)
			}
			if resolvedAssigneeID != proj.LinearAssigneeID {
				logger.Info("resolved assignee", "project", proj.Name, "from", proj.LinearAssigneeID, "to", resolvedAssigneeID)
			}
			if resolvedProjectID != proj.LinearProjectID {
				logger.Info("resolved linear project", "project", proj.Name, "from", proj.LinearProjectID, "to", resolvedProjectID)
			}
			proj.LinearTeamID = resolvedTeamID
			proj.LinearAssigneeID = resolvedAssigneeID
			proj.LinearProjectID = resolvedProjectID
			if err := database.UpdateProject(proj); err != nil {
				logger.Warn("persisting resolved IDs", "project", proj.Name, "error", err)
			}
		}

		if firstLinear == nil {
			firstLinear = lc
			firstGitHub = gc
			firstGitID = &gitIdentity{name: creds.GitAuthorName, email: creds.GitAuthorEmail}
		}

		pollerProjects = append(pollerProjects, poller.ProjectInfo{
			ProjectID:        proj.ID,
			LinearTeamID:     resolvedTeamID,
			LinearAssigneeID: resolvedAssigneeID,
			LinearProjectID:  resolvedProjectID,
			LinearLabel:      proj.LinearLabel,
			LinearClient:     lc,
		})

		ghPollerProjects = append(ghPollerProjects, ghpoller.ProjectInfo{
			ProjectID:     proj.ID,
			GithubOwner:   proj.GithubOwner,
			GithubRepo:    proj.GithubRepo,
			GitHub:        gc,
			TrustedUserID: creds.GithubUserID,
		})

		logger.Info("configured project", "name", proj.Name)
	}

	// --- 5. WebSocket hub ---
	hub := server.NewHub(logger)

	// --- 6. Orchestrator with transitions ---
	sm := orchestrator.New(database)

	if firstLinear != nil {
		invoker := &claudeInvoker{}
		// readOnlyInvoker blocks write tools so the AI can only read the
		// codebase during refinement and iteration — no code changes.
		readOnlyInvoker := &claudeInvoker{
			DisallowedTools: []string{"Edit", "Write", "Bash", "NotebookEdit"},
		}
		cfgLoader := &configLoaderAdapter{}
		puller := &gitPullerAdapter{
			defaultBaseFn: cfgLoader.DefaultBase,
			pullFn:        gitops.PullFFOnly,
		}

		gitOps := &gitOpsAdapter{
			gitAuthorName:  firstGitID.name,
			gitAuthorEmail: firstGitID.email,
		}

		// QUEUED → REFINING
		sm.Register(orchestrator.Transition{
			From: orchestrator.StateQueued,
			To:   orchestrator.StateRefining,
			Action: refine.NewAction(refine.Config{
				Invoker:   readOnlyInvoker,
				Poster:    &linearCommentPoster{client: firstLinear},
				Projects:  database,
				GitPuller: puller,
			}),
		})

		// REFINING → APPROVED (approval check — must be registered BEFORE iteration)
		sm.Register(orchestrator.Transition{
			From:      orchestrator.StateRefining,
			To:        orchestrator.StateApproved,
			Condition: approve.IsApproval(firstLinear),
			Action: approve.NewApprovalAction(approve.Config{
				Comments: firstLinear,
				Projects: database,
			}),
		})

		// REFINING → REFINING (iteration on new comments)
		sm.Register(orchestrator.Transition{
			From:      orchestrator.StateRefining,
			To:        orchestrator.StateRefining,
			Condition: approve.IsIteration(firstLinear),
			Action: approve.NewIterationAction(approve.Config{
				Invoker:   readOnlyInvoker,
				Comments:  firstLinear,
				Projects:  database,
				GitPuller: puller,
			}),
		})

		// APPROVED → BUILDING
		sm.Register(orchestrator.Transition{
			From: orchestrator.StateApproved,
			To:   orchestrator.StateBuilding,
			Action: build.NewAction(build.Config{
				Invoker:    invoker,
				Workspace:  &workspaceCreatorAdapter{pullFn: gitops.PullFFOnly},
				ConfigLoad: &configLoaderAdapter{},
				Linear:     &buildLinearUpdater{client: firstLinear},
				PRDRead:    &buildPRDReaderAdapter{},
				Projects:   database,
			}),
		})

		// ADDRESSING_FEEDBACK → IN_REVIEW
		if firstGitHub != nil {
			sm.Register(orchestrator.Transition{
				From:      orchestrator.StateAddressingFeedback,
				To:        orchestrator.StateInReview,
				Condition: feedback.IsAddressingFeedback,
				Action: feedback.NewAction(feedback.Config{
					Invoker:    invoker,
					Comments:   firstGitHub,
					Replier:    firstGitHub,
					Git:        gitOps,
					Projects:   database,
					ConfigLoad: &configLoaderAdapter{},
				}),
			})

			// FIXING_CHECKS → IN_REVIEW
			sm.Register(orchestrator.Transition{
				From: orchestrator.StateFixingChecks,
				To:   orchestrator.StateInReview,
				Action: checks.NewAction(checks.Config{
					Invoker:    invoker,
					CheckRuns:  firstGitHub,
					Logs:       firstGitHub,
					PRs:        firstGitHub,
					Comments:   firstGitHub,
					Git:        &gitOpsAdapter{},
					Projects:   database,
					ConfigLoad: &configLoaderAdapter{},
				}),
			})

			// IN_REVIEW → IN_REVIEW (rebase when branch falls behind base)
			// Registered after feedback so merge detection and feedback take priority.
			rebaseCfg := rebase.Config{
				Fetcher:  &gitOpsAdapter{},
				Checker:  &gitOpsAdapter{},
				Pusher:   &gitOpsAdapter{},
				Runner:   &rebaseRunnerAdapter{},
				Projects: database,
				Resolver: cfgLoader,
			}
			sm.Register(orchestrator.Transition{
				From:      orchestrator.StateInReview,
				To:        orchestrator.StateInReview,
				Condition: rebase.NeedsRebase(rebaseCfg),
				Action:    rebase.NewAction(rebaseCfg),
			})
		}
	}

	// --- 7. PR and complete actions ---
	var prAction worker.PRCreator
	if firstLinear != nil && firstGitHub != nil {
		gitOps := &gitOpsAdapter{
			gitAuthorName:  firstGitID.name,
			gitAuthorEmail: firstGitID.email,
		}
		prAction = &prActionAdapter{pr.NewAction(pr.Config{
			Invoker:    &claudeInvoker{},
			Git:        gitOps,
			Diff:       gitOps,
			PRD:        &prdReaderAdapter{},
			GitHub:     &ghPRCreatorAdapter{client: firstGitHub},
			Linear:     &linearPoster{client: firstLinear},
			Projects:   database,
			ConfigLoad: &configLoaderAdapter{},
			Rebase:     gitOps,
		})}
	}

	var completeAction ghpoller.CompleteFunc
	if firstLinear != nil {
		completeAction = complete.NewAction(complete.Config{
			Workspace: &workspaceRemoverAdapter{},
			Linear:    &completeLinearUpdater{client: firstLinear},
			Projects:  database,
		})
	}

	// --- 8. Build worker dispatcher ---
	var dispatcherGitName, dispatcherGitEmail string
	if firstGitID != nil {
		dispatcherGitName = firstGitID.name
		dispatcherGitEmail = firstGitID.email
	}
	dispatcher := worker.New(worker.Config{
		DB:             database,
		MaxWorkers:     2,
		LoopRunner:     &loopRunnerAdapter{},
		Projects:       database,
		PR:             prAction,
		GitAuthorName:  dispatcherGitName,
		GitAuthorEmail: dispatcherGitEmail,
		Logger:         logger,
		OnBuildEvent: func(issueID, detail string) {
			if hub == nil {
				return
			}
			msg, err := server.NewWSMessage(server.MsgBuildEvent, map[string]string{
				"issue_id": issueID,
				"detail":   detail,
			})
			if err == nil {
				hub.Broadcast(msg)
			}
		},
	})

	// --- 9. Start pollers ---
	linearPoller := poller.New(database, pollerProjects, 30*time.Second, logger)
	githubPoller := ghpoller.New(database, ghPollerProjects, 30*time.Second, logger, completeAction)

	go linearPoller.Run(ctx)
	go githubPoller.Run(ctx)

	// --- 10. Orchestrator evaluation loop ---
	go runOrchestratorLoop(ctx, sm, database, dispatcher, hub, logger)

	// --- 11. Recover BUILDING issues from previous run ---
	if count, err := dispatcher.RecoverBuilding(ctx); err != nil {
		logger.Warn("recovering building issues", "error", err)
	} else if count > 0 {
		logger.Info("recovered building issues", "count", count)
	}

	// --- 12. Start HTTP server ---
	cfg := server.Config{
		DevMode:          devMode,
		LinearURL:        linearURL,
		GithubURL:        githubURL,
		DB:               database,
		Hub:              hub,
		WorkspaceRemover: &workspaceRemoverAdapter{},
		BuildChecker:     dispatcher,
		PRDPathFn:        workspace.PRDPathForWorkspace,
	}
	srv, err := server.New(addr, cfg)
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	if devMode {
		fmt.Fprintf(os.Stderr, "autoralph listening on %s (dev mode: proxying to Vite)\n", srv.Addr())
	} else {
		fmt.Fprintf(os.Stderr, "autoralph listening on %s\n", srv.Addr())
	}
	if linearURL != "" {
		fmt.Fprintf(os.Stderr, "  Linear API: %s\n", linearURL)
	}
	if githubURL != "" {
		fmt.Fprintf(os.Stderr, "  GitHub API: %s\n", githubURL)
	}

	// Serve in a goroutine so we can wait for shutdown signal.
	go func() {
		if err := srv.Serve(); err != nil {
			logger.Debug("server stopped", "error", err)
		}
	}()

	// --- 13. Wait for shutdown ---
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "\nshutting down...")

	dispatcher.Wait()
	srv.Close()

	return nil
}

// runOrchestratorLoop periodically evaluates state transitions for all active
// issues and dispatches BUILDING issues to the worker pool.
func runOrchestratorLoop(
	ctx context.Context,
	sm *orchestrator.StateMachine,
	database *db.DB,
	dispatcher *worker.Dispatcher,
	hub *server.Hub,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			issues, err := database.ListIssues(db.IssueFilter{})
			if err != nil {
				logger.Warn("listing issues for orchestrator", "error", err)
				continue
			}

			for _, issue := range issues {
				if isTerminalState(issue.State) {
					continue
				}

				// Skip issues with errors — require explicit "Retry" to clear.
				if issue.ErrorMessage != "" {
					continue
				}

				// Re-dispatch BUILDING issues that aren't actively running.
				// This handles retries (state set back to building via API)
				// without requiring a process restart.
				if issue.State == string(orchestrator.StateBuilding) && !dispatcher.IsRunning(issue.ID) {
					if err := dispatcher.Dispatch(ctx, issue); err != nil {
						logger.Warn("re-dispatching building issue", "issue", issue.Identifier, "error", err)
					} else {
						logger.Info("re-dispatched building issue", "issue", issue.Identifier)
					}
					continue
				}

				tr, ok := sm.Evaluate(issue)
				if !ok {
					continue
				}

				logger.Info("executing transition",
					"issue", issue.Identifier,
					"from", tr.From,
					"to", tr.To,
				)

				if err := sm.Execute(tr, issue); err != nil {
					logger.Warn("transition failed",
						"issue", issue.Identifier,
						"from", tr.From,
						"to", tr.To,
						"error", err,
					)

					// Store the error on the issue so the web UI can show it.
					// Only log activity when the error message changes to avoid flooding.
					errMsg := err.Error()
					if issue.ErrorMessage != errMsg {
						issue.ErrorMessage = errMsg
						if dbErr := database.UpdateIssue(issue); dbErr != nil {
							logger.Warn("storing transition error", "issue", issue.Identifier, "error", dbErr)
						}
						database.LogActivity(issue.ID, "transition_error", string(tr.From), string(tr.To), errMsg)

						if hub != nil {
							if msg, wErr := server.NewWSMessage(server.MsgActivity, map[string]string{
								"issue_id":   issue.ID,
								"event_type": "transition_error",
								"detail":     errMsg,
							}); wErr == nil {
								hub.Broadcast(msg)
							}
						}
					}
					continue
				}

				// Re-read issue after transition to get updated state.
				updated, err := database.GetIssue(issue.ID)
				if err != nil {
					logger.Warn("re-reading issue after transition", "issue_id", issue.ID, "error", err)
					continue
				}

				// Clear any previous transition error now that a transition succeeded.
				if updated.ErrorMessage != "" {
					updated.ErrorMessage = ""
					if dbErr := database.UpdateIssue(updated); dbErr != nil {
						logger.Warn("clearing transition error", "issue", updated.Identifier, "error", dbErr)
					}
				}

				// Dispatch BUILDING issues to the worker pool.
				if updated.State == string(orchestrator.StateBuilding) && !dispatcher.IsRunning(updated.ID) {
					if err := dispatcher.Dispatch(ctx, updated); err != nil {
						logger.Warn("dispatching build", "issue", updated.Identifier, "error", err)
					}
				}

				// Broadcast state change via WebSocket.
				if hub != nil {
					if msg, err := server.NewWSMessage(server.MsgIssueStateChanged, updated); err == nil {
						hub.Broadcast(msg)
					}
				}
			}
		}
	}
}

// isTerminalState returns true for states that should not be evaluated by the
// orchestrator (completed, failed, paused).
func isTerminalState(state string) bool {
	switch orchestrator.IssueState(state) {
	case orchestrator.StateCompleted, orchestrator.StateFailed, orchestrator.StatePaused:
		return true
	default:
		return false
	}
}

// prActionAdapter wraps a pr action function to satisfy worker.PRCreator.
type prActionAdapter struct {
	fn func(issue db.Issue, database *db.DB) error
}

func (a *prActionAdapter) CreatePR(issue db.Issue, database *db.DB) error {
	return a.fn(issue, database)
}
