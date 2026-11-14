# The new-app video: from an idea to a live web service (#5514)

Status: draft for Alex to approve, 2026-11-14. Epic #4832, catalogue row F26 (**Let's build
your idea**). No take is made until Alex approves this text.

Alex, 2026-11-14: "Record a video of the \"new app\" experience, using the agent accounts."
Alex's answers (form, 2026-11-14): from the first open to an approved web deploy; the script
first; a new free Sentry account for the agents; a new Google Cloud project on the agents'
Google account, deleted after Alex has watched the video.

## The story

Sam opens Agent Loops for the first time. The app asks one thing: what to build. Sam picks an
idea, a web page with the week's weather for Sam's town. From there an agent does the work,
and Sam does only what only a person can do: connect GitHub, give Google a card, run one
script in Cloud Shell, sign in to Sentry, and approve the deploy. Each of those is a card that
says what to do and why. At the end the web service is live, and Sam has not typed a command
on the Mac.

It is a story, not a tour: each line is one step on the plan page, and the plan page's task
list ticks as the video goes.

- **Scene:** `new-app` (`scripts/video/scenes/new-app.py`), group `usage`, layout
  `mac+browser`, window 1360 × 820, title **"Let's build your idea"**.
- **Why `mac+browser`:** the cards send Sam to outside pages (Google Cloud, Cloud Shell,
  Sentry). The video Chrome beside the window shows those pages, so the viewer sees the step
  the card asks for. Without it the video would show only "Done" presses.
- **Length:** about 95 s after the time cuts, with the title card and the end card.
- **Voice:** Qwen3-TTS **Aiden** (#5079), the directed form of #4964 (`voice.py`).
  `style="warm"` for the first and last lines, `calm` for the rest, and `bright` for "It's
  live."
- **Scratch data only:** the person Sam, the scratch machine "Sam's Mac", and the agents'
  accounts below. No account of Alex's is in the picture.

Decided: group **usage**, with the docs page `how-to/build-your-first-app/` (from
`docs/how-to/build-your-first-app.md`), because this is the first thing a new person does,
step by step, as U1 was. Alex can overrule.

Decided: the idea is **"A web page that shows this week's weather for my town"** from the
question page's own list, and the name is **Town Weather**, because an idea from the list
picks its starter with no doubt (#5048, section 3) and it goes with the `weather` folder of
U1 and the GitHub video. Alex can overrule.

## The script

Each line starts at its marker. `fast(n)` is a span cut to `n` seconds. *What is said* is the
directed line, as it goes into `LINES` in the scene; the words alone are what Whisper checks.

| # | Marker | What is shown | What is said | Time |
|---|---|---|---|---|
| — | — | Title card: the logo, **Let's build your idea**, "Loops · How to". Fades into the window. | — | 3 s |
| 1 | `intro` | The first open: no sidebar, no toolbar. The Huddle, **Let's build your idea.**, the prompt box, with its placeholder going through the ideas. | `<speak style="warm">The first time Sam opens Agent Loops,<break time="250ms"/>it asks one thing:<break time="350ms"/><prosody rate="92%">what do you want to build?</prosody></speak>` | 6 s |
| 2 | `idea` | The placeholder stops on *A web page that shows this week's weather for my town*. Tab puts it into the box. **Submit**. | `<speak style="calm">Sam takes one of the ideas,<break time="200ms"/>and clicks <emphasis>Submit</emphasis>.</speak>` | 4 s |
| 3 | `plan` | The plan page: the idea as its title, *A web service, from agentloops-dev/web-service-starter* with **Change**, and the seven tasks, each **To do**. | `<speak style="calm">The plan page shows every step,<break time="200ms"/>from here to a web service that's live.</speak>` | 5 s |
| 4 | `github` | Task 1, **Set up the agent runtime**: Claude, **Done**. Task 2: **Connect GitHub…**; the sheet; `fast(2)` over the device sign-in as Sam in the browser; **Done**. | `<speak style="calm">Two steps are Sam's own:<break time="200ms"/>an agent runtime,<break time="150ms"/>and GitHub.<break time="400ms"/>Claude is ready,<break time="150ms"/>so Sam connects GitHub.</speak>` | 8 s |
| 5 | `name` | *Starting the agent…*, then the agent's chat under the list. A question card: *What is the app's name?* Sam types **Town Weather** and sends it. | `<speak style="calm">Then an agent takes over.<break time="300ms"/>It asks one question:<break time="200ms"/>the app's name.</speak>` | 5 s |
| 6 | `repo` | `fast(3)`: the agent's steps: the repository `town-weather` from the starter, the gate `production`. Task 3 **Done**. | `<speak style="calm">It makes a private repository from the starter,<break time="250ms"/>and a gate,<break time="150ms"/>so nothing deploys until Sam says yes.</speak>` | 7 s |
| 7 | `cloud` | Task 4 **Waits for you**. The card: *Sign in to Google Cloud, and add a billing account with your card*, with its reason. The browser shows the Google Cloud console's billing page, signed in. **Done**. | `<speak style="calm">Where only Sam can act,<break time="150ms"/>a card says what to do,<break time="150ms"/>and why.<break time="400ms"/>Sam pays Google.<break time="200ms"/>The agent never sees the card.</speak>` | 8 s |
| 8 | `shell` | The next card: *In Cloud Shell, run bash setup-gcp.sh, and click Authorize when Cloud Shell asks*. The browser: Cloud Shell, `setup-gcp.sh` uploaded by the agent; `fast(3)` over the script's run, to its last two lines. **Done**. Task 4 **Done**. | `<speak style="calm">The agent puts a script in Cloud Shell.<break time="250ms"/>Sam runs it,<break time="200ms"/>and it sets up the deploy,<break time="200ms"/>with no key to copy.</speak>` | 8 s |
| 9 | `sentry` | Task 5 **Waits for you**. The card: *Sign in to Sentry, or sign up on the free plan*; the browser: Sentry, signed in; **Done**. `fast(3)`: the Sentry project `town-weather`, its DSN set as a CI variable. Task 5 **Done**. | `<speak style="calm">Sentry is next.<break time="300ms"/>Sam signs in;<break time="200ms"/>the agent makes the project,<break time="150ms"/>and puts it in the repository's settings.</speak>` | 8 s |
| 10 | `approve` | Task 6 **Done**: the tag `v0.1.0`. Task 7 **Waits for you**. **Open Agent Loops**: the usual window. **Needs You**: the deploy to `production` waits. Sam opens it, reads what ships, **Approve**. | `<speak style="calm">It tags the first version.<break time="350ms"/>The deploy waits in <emphasis>Needs You</emphasis>,<break time="200ms"/>until Sam approves it.</speak>` | 8 s |
| 11 | `live` | `fast(4)` over the run. The agent's reply: the Cloud Run address. The browser opens it: the starter's page, **Town Weather**. | `<speak style="bright">A few minutes later,<break time="200ms"/>it's live.</speak>` | 4 s |
| 12 | `end` | The usual window: the folder `town-weather` in the sidebar, the session's plan strip with all seven tasks **Done**. | `<speak style="warm"><prosody rate="90%">From an idea<break time="200ms"/>to a live web service.<break time="450ms"/>Next,<break time="150ms"/>Sam just says the features.</prosody></speak>` | 6 s |
| — | — | End card: the logo, **Loops**, "docs.agentloops.dev/how-to/build-your-first-app/". | — | 3 s |

About 160 words over about 80 s of lines, near the calm pace Alex chose for U1.

## Is it true today?

Each claim checked against the code and the specs on 2026-11-14. The take checks each again
and stops at the first one that is false; then the script changes, not the product.

| Line | Claim | True today? |
|---|---|---|
| 1 | The first open shows the question page. | Yes on the Mac: `FirstBuildQuestionView` (#5053), for a newcomer only (no folder, no agent ever run, #5048 section 9). **The recorder has no first open:** `make.py` launches a scratch root that is set up and paired. The scene needs a launch with an empty control plane, with the machine lock approved and the recovery key skipped behind the question page (section 8 of #5048: the set-up runs behind it). New in `make.py`. |
| 2 | Tab puts the idea that shows into the box. | Yes, by the spec (#5048 section 1). The scene waits until the placeholder shows that idea, then presses Tab. |
| 3 | The plan page and its seven tasks. | Yes: `FirstBuildPlanView` and `FirstBuild.starterLine` ("A web service, from agentloops-dev/web-service-starter"). |
| 4 | Claude is **Done** at once; GitHub is Sam's step. | Claude is Done when it is installed and signed in on the scratch host (it is, as in U1). Connect GitHub is the device flow of the `agents-loops` App, done as Sam (`agentloops-dev-agent`) in the video Chrome. **Needs `GITHUB_SAM_TOKEN` and the password without Ask before use** (#5076 waits on the same), and the agents' own `git` and `gh` on the scratch host signed in as Sam, not as alexec. |
| 5 | The agent asks only the name. | Yes: the first-build prompt names the kind and the starter, and the skill skips a question that is answered (#5048 section 3). |
| 6 | A private repository and a gate. | Yes: `make_app_repository` (private, #3708) and `set_environment_gate` `production` with Sam as the reviewer (#3706). Under Sam's account the repository is `agentloops-dev-agent/town-weather`. |
| 7 | The card, and "the agent never sees the card". | Yes: `hand_to_person` (#3529) with the skill's words; the agent never types a card (the skill's rules). In the take, the Loops Agents Google account has its billing account set up before the take, with `PRIVACY_CARD`, so the page shows it signed in and billed. |
| 8 | The agent uploads the script to Cloud Shell; "no key to copy". | The upload is the skill's step 5.2 (`browser_file_upload`), which the skill says can fail ("the next card says to paste the file"). **Not walked yet:** if the upload fails in the take, line 8 says "Sam pastes a script into Cloud Shell" instead. "No key": true, the script sets up Workload Identity Federation (#3654). The project number on the last line is not a secret. |
| 9 | Sentry project and DSN. | Yes, by the skill's step 6 with Sentry's MCP server. **Two parts are cut** from the picture: adding the Sentry connector (step 6.2), done in the take's set-up before the recording, and the events token (step 6.5), which the scene answers with a token made for the video, blurred, checked and revoked (#1793's three defences). If the connector cannot be added on a scratch host, the line drops "makes the project" and says "it sets up Sentry". |
| 10 | The deploy waits in Needs You; Sam approves. | Yes: `deploy.waiting` and the approval in Needs You (#3587, #3588). Only the person can approve. |
| 11 | "A few minutes later, it's live." | The first Cloud Run deploy of the starter: a container build and a deploy, about 3 to 6 minutes, cut. The starter's page shows the app's name; the video does not say it shows the weather (no feature is built yet). |
| 12 | The plan strip with all tasks **Done**. | Yes: the session keeps the plan (#5052), and its plan strip draws it. |

## What the take needs

1. **The first open in the recorder** (`make.py`): a scratch root with an empty control
   plane, so the window shows the question page; its machine lock approved and its recovery
   key skipped behind it, with nothing on camera.
2. **Sam's GitHub sign-in:** `agentloops-dev-agent`, its token and password with Ask before
   use off (Alex's step, as for #5076), and the scratch host's `gh` and `git` signed in as Sam
   (`GH_CONFIG_DIR` in the scratch root). The repository `town-weather` is deleted after each
   take, so the next take makes it again (`make_app_repository` would skip one that is there).
3. **Google Cloud:** the Loops Agents account with a billing account on `PRIVACY_CARD`, set up
   once before the first take. Each take makes a new project `town-weather-NNNN` (the
   skill's rule); the scene deletes it after the take (`gcloud projects delete`), and the
   last one after Alex has watched. Cloud Run's free tier covers one small service, so the
   cost is about $0.
4. **Sentry:** the agents' new free account (agentloops@fastmail.com, the sign-up skill),
   signed in in the video Chrome; the Sentry connector on the scratch host before the
   recording. The project `town-weather` is deleted after each take.
5. **The video Chrome profile** `~/.agents/video/chrome/new-app`, signed in to GitHub as Sam,
   to Google as Loops Agents, and to Sentry. The agents' browser of the scratch host must be
   the same Chrome, so that the pages it opens are the ones in the picture. **Not walked
   yet:** which browser a scratch host's `browser_*` tools drive. If it is not the video
   Chrome, the scene shows each card's page in the video Chrome itself, with the same
   address.
6. **Secrets in the picture:** none is typed on camera except the Sentry events token
   (line 9), made for the video and revoked after. The Cloud Shell page shows the project
   number and the service account's address: not secrets, but the OCR check is told about
   neither.
7. **Time cuts** (`v.fast`): the GitHub device flow, the agent's repository work (about
   1 min), the Cloud Shell script (about 2 min), Sentry (about 1 min), and the deploy (about
   3 to 6 min). One take is about 15 to 20 min of wall time.

## Its size

L (spec section 1.1): real accounts, tokens made, blurred, checked and revoked, and a new
recorder start (the first open). About half a day for the take, after the accounts.
