# GitHub App Setup

AutoRalph can authenticate with GitHub using either a personal access token
(PAT) or a GitHub App. This guide walks through creating and installing a
GitHub App, which is the recommended approach for organizations.

## Why Use a GitHub App?

| | Personal Access Token | GitHub App |
|---|---|---|
| **Token lifetime** | Long-lived (until revoked) | 1-hour auto-rotating tokens |
| **Permission scope** | Broad (tied to your account) | Scoped to specific repos |
| **Rate limits** | Shared with your personal usage | Separate pool (5,000/hr per installation) |
| **Audit trail** | Actions attributed to you | Actions attributed to the app |
| **Org installation** | Requires your PAT to have org access | Installed per-org with owner approval |

For personal repos, a PAT works fine. For organizations, a GitHub App is
strongly recommended.

## Step 1: Create the App

1. Go to [GitHub Developer Settings](https://github.com/settings/apps) →
   **New GitHub App**

2. Fill in the basics:

   | Field | Value |
   |-------|-------|
   | **GitHub App name** | Something unique, e.g., `AutoRalph - YourName` |
   | **Homepage URL** | Any valid URL (e.g., your repo URL) |

3. Under **Webhook**:
   - **Uncheck** "Active" — AutoRalph uses polling, no webhooks needed

4. Under **Permissions → Repository permissions**, grant:

   | Permission | Access |
   |------------|--------|
   | **Contents** | Read and write |
   | **Pull requests** | Read and write |
   | **Issues** | Read and write |

   Leave all other permissions at "No access".

5. Under **Where can this GitHub App be installed?**:
   - Choose **Only on this account** if you only need it for your personal repos
   - Choose **Any account** if you need to install it on an organization (see
     [Installing on an Organization](#installing-on-an-organization) below)

6. Click **Create GitHub App**

## Step 2: Get the Client ID

After creation, you'll land on the app's settings page. Copy the **Client ID**
— it starts with `Iv` (e.g., `Iv23liXXXXXX`).

This goes into your credentials as `github_app_client_id`.

## Step 3: Generate a Private Key

1. On the app settings page, scroll to **Private keys**
2. Click **Generate a private key**
3. A `.pem` file will download — save it somewhere safe:

   ```bash
   mv ~/Downloads/your-app-name.*.pem ~/.autoralph/github-app.pem
   chmod 600 ~/.autoralph/github-app.pem
   ```

This goes into your credentials as `github_app_private_key_path`.

> **Keep this file safe.** Anyone with the private key can authenticate as your
> app. Never commit it to version control.

## Step 4: Install the App

### Installing on Your Personal Account

1. From the app settings page, click **Install App** in the sidebar
2. Select your personal account
3. Choose repository access:
   - **All repositories** — grants access to everything
   - **Only select repositories** — pick specific repos (recommended)
4. Click **Install**

### Installing on an Organization

To install on an org, the app must be **public** (set to "Any account" in Step
1). If you created it as private, change it:

1. Go to your app settings → **Advanced** (bottom of sidebar)
2. Under "Danger zone", click **Make public**
3. Confirm

> **"Public" does not mean anyone can use your app.** It means anyone can
> *install* it, but they cannot authenticate as it without your private key.
> Since AutoRalph uses polling (no webhooks), an unauthorized installation
> would have zero effect.

Then install it:

1. Go to `https://github.com/apps/<your-app-name>/installations/new`
2. Select the organization
3. Choose repository access (only select repositories is recommended)
4. If you're an **org owner**: the installation happens immediately
5. If you're **not an org owner**: GitHub sends an installation request to the
   org owners for approval — you'll need to coordinate with them

> **Tip for org procurement**: The app introduces no new vendor, no webhooks,
> no external servers. It's a self-hosted tool using GitHub's built-in App
> platform. The only change is granting API access to specific repos.

## Step 5: Get the Installation ID

After the app is installed:

1. Go to `https://github.com/settings/installations` (for personal) or the
   org's **Settings → Integrations → GitHub Apps** page
2. Click **Configure** next to your app
3. The URL will look like: `https://github.com/settings/installations/12345678`
4. That number (`12345678`) is your installation ID

This goes into your credentials as `github_app_installation_id`.

> **Different orgs = different installation IDs.** If you install the same app
> on your personal account and your work org, each gets its own installation
> ID. Use separate credential profiles for each.

## Step 6: Configure AutoRalph

Add a credentials profile in `~/.autoralph/credentials.yaml`:

```yaml
profiles:
  work:
    linear_api_key: lin_api_yyyyyyyyyyyyy
    github_app_client_id: "Iv23liXXXXXX"
    github_app_installation_id: 12345678
    github_app_private_key_path: ~/.autoralph/github-app.pem
    github_user_id: 87654321
    git_author_name: autoralph
    git_author_email: autoralph@myorg.com
```

Then reference it in your project config:

```yaml
# ~/.autoralph/projects/my-project.yaml
name: my-project
local_path: ~/code/my-project
credentials_profile: work
# ...
```

All three `github_app_*` fields are required — partial configuration is an
error. See [Configuration](configuration.md) for the full reference.

## Verifying the Setup

Start AutoRalph and check the logs for successful GitHub API calls:

```bash
autoralph serve
```

You can also verify the app can access your repos by checking the dashboard at
`http://127.0.0.1:7749` — project status should show as healthy.

If you run into authentication errors, verify:

1. The private key file path is correct and the file is readable
2. The installation ID matches the org/account you're targeting
3. The client ID matches your app (not the App ID — look for the one starting
   with `Iv`)
4. The app has the required permissions (Contents, Pull requests, Issues —
   all read/write)
5. The app is installed on the repositories your project config references

## Managing Installations

### Changing Repository Access

After installation, you can adjust which repos the app can access:

1. Go to the installation's configure page (org settings or personal settings)
2. Under **Repository access**, add or remove repos
3. Changes take effect immediately

### Revoking Access

To uninstall the app from an org or account:

1. Go to the installation's configure page
2. Click **Uninstall** at the bottom
3. The app loses all access immediately

### Rotating the Private Key

If you need to rotate the private key:

1. Go to your app settings → **Private keys**
2. Click **Generate a private key** (creates a new one)
3. Update `github_app_private_key_path` in your credentials to point to the
   new `.pem` file
4. Restart AutoRalph
5. Delete the old key from the app settings page
