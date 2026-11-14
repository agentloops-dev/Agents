# A puppet Alex: speak to the app and hear it, with no person (#4945)

Status: decided, except the one install in "Speak (Mac)", which is Alex's (form open,
2026-11-14). Spike of epic #4944, milestone `1.0 Public release`. Probes: `probes/`.

## The question

How does an agent **speak to** the app and **hear** it with no person, through the real
audio path (the microphone, the transcriber, echo cancellation, the speaker), on the Mac,
the iPhone and the iPad? And what does it read to measure the epic's bar?

## What the probes found (this Mac, macOS 27.0.1, 2026-11-14)

The probe is `probes/tapmic.swift` (`swiftc -O -o /tmp/puppet-4945/tapmic tapmic.swift`).
It plays a `say` line with `afplay`, taps that process, and records the tap.

| # | Test | Result |
|---|---|---|
| 1 | A Core Audio **process tap** of `afplay` (`CATapDescription`, `muteBehavior = .muted`) in a public aggregate device, read by an IOProc | Works. 576 512 samples in 6 s at 48 kHz × 2, peak 0.74. Whisper (`whisper-small.en-mlx`) gave the line word for word. The speakers played nothing. No permission prompt came up. |
| 2 | That aggregate device as the input of `AVAudioEngine` (`auAudioUnit.setDeviceID`) | Refused: -10851 (`kAudioUnitErr_InvalidPropertyValue`) from another process; ignored with no error in the same process (peak 0). |
| 3 | That aggregate device as the input of the **voice-processing unit** (`kAudioUnitSubType_VoiceProcessingIO`, `kAudioOutputUnitProperty_CurrentDevice`, every scope) | Refused: -10851. With voice processing on, the engine records the **default input** only. |
| 4 | That aggregate device as the system's default input | Refused: `kAudioDevicePropertyDeviceCanBeDefaultDevice` is 0, and the set does nothing (also with a speaker-only sub-device as its clock). |
| 5 | The voice-processing unit with **a real other device** (the MacBook Air microphone, the Samson Q2U) | Accepted: status 0, and `AudioUnitInitialize` 0. |
| 6 | Which process has a microphone open (`kAudioProcessPropertyIsRunningInput` on the process objects) | Works. `kAudioDevicePropertyDeviceIsRunningSomewhere` does not: Display Audio is also the output, so it says "in use" while anything plays. |

So:

- **Hearing** needs no install: a process tap of the app, muted, read by an IOProc, then
  Whisper (test 1).
- **Speaking** into the real path needs a **real** input device (tests 2 to 5). A tap
  cannot be a microphone for the app. Voice mode always runs with voice processing on,
  so it is no use to give only Dictate (which has no voice processing) a tap.
- Apple's `ParrotAudioPlugin.driver` is in `/Library/Audio/Plug-Ins/HAL` (an OS part, from
  2026-09-24) but shows no device. We do not use it.

Lessons from the probes, for #4947:

- A run that fails must still destroy its tap and its aggregate device. Two failed probe
  runs left two public "Puppet Mic" devices on this Mac until `tapmic clean` removed them.
  The puppet names its devices `dev.agentloops.puppet.<uuid>` and removes every one at
  start and at end.
- Never change the system's default input while a process has a microphone open (test 6):
  it can be Alex's own voice mode or a call. The probe refused to swap, correctly.
- A recording of the default input is a recording of Alex's room. Check the device before
  recording, and delete every such file at once.

## Speak (Mac)

Options, the chosen one first.

1. **A virtual audio driver, fed by the puppet, chosen by the test build only.** *Chosen,
   if Alex approves the install.* The puppet writes TTS speech (`say`, with more than one
   voice and rate; never Kokoro, so the puppet's voice is not the app's) to the driver's
   output, and the driver loops it to its input. The test build takes that input as its
   microphone by a test-only setting (below), **not** by the system's default input, so
   Alex's own apps keep his microphone. Test 5 says the voice-processing unit takes a real
   device that is not the default.
   - **BlackHole 2ch** (`brew install --cask blackhole-2ch`, v0.7.1, 2026-07-03; about 20 000
     stars). GPL-3.0: tooling only, on this Mac, never in an app or in this repo, so it fits
     AGENTS.md ("no GPL/AGPL in the apps"). Recommended in the form.
   - **Our own driver** from Apple's AudioServerPlugIn sample ("Creating an Audio Server
     Driver Plug-in", Apple's sample licence). No GPL, but one more task before #4947, and
     we keep a driver.
   - Both are an **install on Alex's Mac**: the admin password, a file in
     `/Library/Audio/Plug-Ins/HAL`, and a Core Audio restart or a reboot. They add a device to
     Sound settings. This is Alex's to approve (form, 2026-11-14).
   - It does not test the room: no real microphone, no room noise, no acoustic echo. The
     echo test is made digital: the puppet mixes the app's tapped voice (Hear, below) back
     into the driver's input, with a delay (about 30 ms) and a gain (about -12 dB). Voice
     processing has the app's own output as its reference, so it must cancel that mix as it
     cancels a room's echo. A run with no mix is the control.
2. **The Mac's speaker into the Mac's real microphone (acoustic).** No install. It tests
   all of the path, the room and real echo. But it makes sound, it needs a quiet room and
   Alex away (`~/.agents/memory/drive-the-scratch-app-only-when-alex-is-away.md`: idle for
   minutes, the screen unlocked, no process with a microphone open), and it uses the
   default input, so it also needs the system's default input to be the microphone that
   the test build uses. Use it as a **second tier**: a small acoustic run, a few scenarios,
   at night, to check that the digital runs agree with a real room. It is the only Mac
   option if Alex says no to the install.
3. **An in-app audio feed in a test build** (a WAV into the transcriber in place of the
   microphone). No install, no sound, works with a locked screen and with Alex at the Mac.
   But it skips the microphone, the HAL and voice processing, which are where the epic's
   failures may be. *Not chosen* as the puppet. It stays useful for #4949's transcriber
   scenarios (words lost at a pause, product words) if option 1 is refused.

## Hear (Mac)

**Chosen: a muted process tap of the test build, read by an IOProc (test 1), then Whisper**
(`mlx-whisper`, `whisper-small.en-mlx`, as `scripts/video/voice.py` uses, in its venv
`~/.cache/agents-video/venv`). The tap is `muteBehavior = .muted`, so nothing plays on
Alex's speakers, and the voice is heard at its real time, from the real player, with voice
processing on its output.

Also read, because Whisper alone cannot say why:

- **`said`, `heard` and `phase`** from the walk socket's `{"cmd":"voice"}` (read only; a
  puppet run never sends `"walk": true`, which turns the microphone and the speaker off).
- **The voice log**: the `voice:` and `dictation:` lines in the unified log
  (`/usr/bin/log show --predicate 'subsystem == "<bundle id>" AND category == "voice"'`,
  #2700), each with its time.

Not chosen: ScreenCaptureKit audio. It also needs Screen Recording permission, and
"including app" takes every process with the bundle id
(`.agents/memory/screen-recording-catches-the-real-window.md`), so it can catch Alex's own
window. A tap is by process object (pid), and so takes the scratch app only. The walk-only
tape (#4854) is for walks, so it is not the real player.

## State and timing (Mac)

What the puppet reads, for each measure of the epic's bar:

| Measure | From where |
|---|---|
| Listening after the press (100 ms) | the press's time (the puppet presses) → the `voice: listening` (or `dictation: listening`) log line |
| Words lost; prompt sent twice or split; word error rate | the text the agent got: the scratch daemon's transcript of the session (read over its `daemon.sock`), against the scenario's script, scored as WER (`jiwer`, Apache-2.0, or the same small Levenshtein the video tools use) |
| Echo heard as a prompt; false barge-in | the transcript (a prompt that holds the voice's words) and the log (a barge-in while the puppet was silent) |
| Talking over the voice stops it (300 ms) | the puppet's speech start → the tap's sound stopping (the tap's samples fall to silence) and the log's stop line |
| First sentence spoken (1 s) | the turn's first text in the transcript → the tap's first sound |
| The app stops or hangs | the pid ends, the walk socket closes, or a new `Agents Host-*.ips` in `~/Library/Logs/DiagnosticReports`; a hang is no walk-socket reply for 5 s |

All times are on the Mac's one clock (`mach_continuous_time`, or seconds since 1970 in
every file), so there is nothing to line up.

**Presses** go through the walk socket (`walk.py press "=Voice mode"`) or AX by pid, never
keystrokes to the front app (`~/.agents/memory/keystrokes-go-to-the-front-app-not-the-scratch-one.md`).
The socket and the tap work with Alex at the Mac and with a locked screen, so option 1
needs Alex neither away nor unlocked.

## iPhone and iPad

1. **Speak: the Mac's speaker beside the real iPhone or iPad** (acoustic). *Chosen.* It is
   the only way into a real device's microphone, its hardware voice processing and its
   audio session's route. A virtual driver on the Mac does not reach a device. It makes
   sound, so the same rule as Mac option 2: Alex away, a quiet room.
2. **The iOS simulator.** It takes the Mac's audio input, so with the driver it could hear
   the puppet silently. But it does not test the device's hardware voice processing, its
   routes (speaker, receiver, Bluetooth HFP), the audio session's interruptions, or the
   on-device speech models as a phone has them, and this Mac has no Simulator.app to choose
   its input (`~/.agents/memory/no-simulator-gui-on-this-mac.md`), so it would need the
   system's default input to be the driver (see the default-input rule above). *Not
   chosen* as the iOS puppet. #4948 may use it for a quick check of the Remote's own code
   (the walk socket runs there), if it proves the simulator hears the driver.

**Hear (iOS):** the Mac's microphone records the device's speaker (acoustic; the Mac's
default input, so the same rule), then Whisper; and `said` and the `voice:` lines from the
Remote's `Library/Caches/remote.log`, copied with `devicectl` after the run.

**Presses (iOS):** the Remote's walk socket is in simulator builds only
(`Remote/Sources/Walk/WalkCommands.swift`, `#if DEBUG && targetEnvironment(simulator)`).
On a device, #4948 adds a way in. Recommended: an XCUITest runner on the device
(`xcodebuild test-without-building`), which taps the Remote's own elements by label and
needs no new socket in the app. The device must be unlocked and awake, with the Remote in
front: Alex's hands (below).

**State and timing (iOS):** the device's clock and the Mac's differ. The puppet puts a
short tone at the start of each run into the speaker; the device's log line for the
first sound heard and the Mac's play time give the offset. `remote.log` lines have times.

## Kokoro's stop (#4853)

#4853 stops the app in Kokoro's Core ML run (`libBNNS`) while it makes the first reply
sentence. It is Apple's (FluidInference/FluidAudio#889), seen in walks; the puppet's runs
are not walks, so they may or may not see it. The puppet copes:

- It sees a stop (the pid ends, or a new `.ips`) and records the run as `stopped`, with the
  `.ips` file's name and its top frames.
- It classifies a stop whose faulting frames are `BNNSGraphContextExecute_v2` under
  `Espresso` as **`#4853`**. The bar's "the app stops" counts every stop, and the report
  shows the #4853 ones apart, so the other measures of that run's scenario still count from
  the runs that did not stop.
- It starts the test build again and goes on with the next run. A suite never ends at the
  first stop.
- Scenarios that need no spoken reply (dictation, "listening within 100 ms") run with the
  voice off, so #4853 cannot hide their failures.

## What stays Alex's test

- **#4951, the one final test** on the installed apps, by ear and by voice: the real room,
  his voice and accent, his AirPods, a real call coming in.
- Bluetooth routes (AirPods, HFP) on all devices: the puppet has no Bluetooth headset.
- The installed app's own permission prompts (Microphone, Speech Recognition) as a new
  person sees them.

## Human steps left, and their autonomy issues (#3905)

| Step | Who, today | Issue |
|---|---|---|
| Approve and install the virtual audio driver (admin password, Core Audio restart) | Alex, once | the form of 2026-11-14; see below |
| Grant Microphone and Speech Recognition to the test build's signature, once | Alex's click on the system prompt (MDM cannot grant these) | #5544 |
| Grant System Audio Recording (the tap) to the app that runs the puppet, once on a new Mac | Alex's click (here it was granted already) | #5545 |
| Unlock the iPhone and iPad, keep them awake, the Remote in front, for an acoustic run | Alex's hands | #5546 |
| A quiet room, and Alex away, for an acoustic run | Alex | the puppet checks idle time, the lock and the microphone itself (no issue: it is a schedule, not a step) |

## What the rest of the epic must change

- **#4946 (real voice in a test build):** add a **test-only input device**: a DEBUG (or
  test-signed build) setting, for example the environment `AGENTS_VOICE_INPUT_UID=<uid>`,
  that `VoiceAudio` and `Dictation` use to set `kAudioOutputUnitProperty_CurrentDevice` on
  the input unit, with voice processing on. Prove that voice processing records from it
  (test 5 proved only that it takes it). Also a stable signature for the test build, so the
  Microphone and Speech Recognition grants stay from run to run. #4853's walk crash is not
  this task's.
- **#4947 (the Mac puppet):** `scripts/voice/puppet.sh` (or Swift, from `probes/tapmic.swift`):
  the driver as its mouth, the tap as its ears, the walk socket for presses and state, the
  log and the daemon's transcript for times and text. It removes its taps and devices at
  start and at end, never changes the default input, and refuses to run when the driver is
  missing, with the line that says so. The digital echo mix is its job.
- **#4948 (the iOS puppet):** acoustic, on the real devices; an XCUITest runner for presses;
  `remote.log` and Whisper for hearing; the tone for the clocks. It runs only when the
  puppet's "away" check passes. Its title can say "real devices", not the simulator.
- **#4949 (the suite):** each Mac scenario runs digitally (20 runs) and a few acoustically
  (a second tier, at night); the report says which. Stops are scored as above.
- **#4950 (the PR check):** runs only on a Mac that has the driver, before push, as
  `scripts/go-changed.sh` does. CI's runners have no driver and no microphone.
- **#4853:** no change; the puppet copes with it as above.
