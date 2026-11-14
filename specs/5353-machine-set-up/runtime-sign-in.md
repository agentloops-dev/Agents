# Runtime sign-in on a new machine (spike #5360, epic #5353)

Answers the "Sign-in" section of the epic's spec. Read on 2026-11-14 from each runtime's
own documentation and from what the daemon already does (`Go/internal/agentsd/runtimesignin.go`).
"Not in docs" means the vendor's page does not say; the step must measure it (slice 6).

## Table

| Runtime | Key accepted? | Where it goes, as the machine's user | Device or browser route | What the code shows, and how long | Copy a login from the Mac? |
|---|---|---|---|---|---|
| **Claude** | Yes. `ANTHROPIC_API_KEY` (Console key; in `-p` mode always used). | Environment of the run (the daemon already clears and sets it, `agentsd`). Nothing is written to disk; `/login` would write `~/.claude/.credentials.json` (0600). | No device code. `claude setup-token` opens a browser link; the page gives a code typed back into the terminal. It prints a **one-year** `CLAUDE_CODE_OAUTH_TOKEN` (model requests only; needs a paid plan). | A link and a paste-back code. Lifetime of the code: not in docs. The daemon waits 15 min. | No. Docs describe one config dir per login and no several-machine login. |
| **Codex** | Yes. `printenv OPENAI_API_KEY \| codex login --with-api-key`. | `~/.codex/auth.json` (plaintext by default) or the OS store; `cli_auth_credentials_store` picks. | `codex login --device-auth` (beta, the documented choice for headless). Needs device-code login switched on in the ChatGPT account or workspace. | A link and a one-time code. Lifetime: not in docs. The daemon waits 15 min. | The docs offer copying `auth.json` as a fallback, with "treat it like a password". Not a several-machine login. A ChatGPT Enterprise **access token** (`codex login --with-access-token`) is documented for trusted runners: a later option for orgs. |
| **Copilot** | Yes, a token, not an API key: `COPILOT_GITHUB_TOKEN`, then `GH_TOKEN`, `GITHUB_TOKEN`. A fine-grained PAT (`github_pat_`) with the **Copilot Requests** permission, an OAuth `gho_` or an app `ghu_` token. Classic `ghp_` is refused. | Environment (overrides a stored login). Stored login: OS keychain `copilot-cli`; with no keychain, `~/.copilot/config.json` in plaintext. | `/login` ▸ device code at github.com/login/device. | One-time code. GitHub's device codes last 15 min (GitHub's OAuth docs; not on the Copilot page). | No. |
| **Cursor** | Yes. `CURSOR_API_KEY` or `--api-key`, from the Cursor Dashboard. | Environment or flag. Stored login: "locally", path not documented. | `agent login` opens a browser; `NO_OPEN_BROWSER=1` prints the URL. **No device code.** | A URL only. Not in docs. | No. |
| **Grok** | Yes. `XAI_API_KEY` (order: `model.api_key`, `model.env_key`, session token, `XAI_API_KEY`). | Environment. Config is `~/.grok/config.toml` (or `$GROK_HOME`). | `grok login` (browser) and `grok login --device-auth` for ssh and headless hosts. | One-time code. Lifetime: not in docs. | No. Where the session token is kept: not in docs. |
| **OpenCode** | Yes, per provider. Provider env vars (`OPENROUTER_API_KEY`, `AWS_BEARER_TOKEN_BEDROCK`, …), or `opencode auth login` for a key typed into its store. It works signed out on Zen's free models. | Its own `auth.json` (path not on the providers page) or the provider's variable. | Only for some providers: GitHub Copilot uses GitHub's device code (15 min). The rest take a key. | As Copilot for Copilot. | No. |

## Result

- **Every runtime takes a key** (Copilot a token). A key is the only route that needs no person, so step 7 can be automatic for a runtime when the person has put its key in Secrets.
- **Only Codex, Grok and Copilot have a true device code** (Copilot and OpenCode-Copilot through GitHub). **Claude** has a browser link with a paste-back code. **Cursor** has a browser URL only. These are the sign-ins where step 7 says **waiting for you**.
- **No runtime documents one login on several machines**, so nothing is copied from the Mac. Codex's `auth.json` copy and the Enterprise access token are noted, not built.
- **Where to write.** Prefer the run's environment (no file on the machine) for Claude, Cursor, Grok, Copilot and OpenCode; Codex's own `codex login --with-api-key` for Codex, which only reads `auth.json`. All as the machine's user, never root.

## Secret names step 7 looks for

`ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` (Claude), `OPENAI_API_KEY` (Codex), `COPILOT_GITHUB_TOKEN` (Copilot), `CURSOR_API_KEY` (Cursor), `XAI_API_KEY` (Grok), and the provider's own variable (OpenCode).

## Not settled by reading (slice 6 measures it)

Device-code lifetimes for Codex, Grok and Cursor's URL; where Cursor and Grok keep a stored login; whether Grok's ACP mode accepts `XAI_API_KEY` set in the environment alone on Linux.
