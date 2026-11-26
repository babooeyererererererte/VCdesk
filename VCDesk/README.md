# VC Desk — Windows desktop voice & video

VC Desk is a Windows desktop application built with Go + Wails. The interface is embedded in the desktop app; it does not open a normal browser tab. The Windows build workflow compiles the application and its small signaling server on a hosted Windows runner, so the end user does not need Go, Node.js, or a compiler installed.

## Features

- Two-person voice and video rooms using WebRTC
- Camera and microphone permission handling
- Microphone mute/unmute and camera on/off
- Local and remote video preview
- Room codes and display names
- Leave-call control and connection status/error messages
- Separate signaling server with `/health` endpoint

## Important networking note

The signaling server is required. `VCDesk.exe` does not magically create an internet service. For a same-computer/LAN test, run `vcdesk-signaling.exe` on one machine and enter `ws://HOST-IP:8080/ws` in both clients. For internet use, deploy the signaling server on a public host with TLS and enter its `wss://.../ws` address. Some restrictive networks also require a TURN relay; the client currently includes public STUN servers only, so a TURN service may be needed for reliable connectivity across all networks.

The signaling server is a minimal prototype. Before public production deployment, add authentication, rate limiting, an explicit allowed-origin list, TLS termination, and a configured TURN service. Do not expose it publicly as-is for sensitive calls.

## Build without installing tools on the user's PC

1. Create a private or public GitHub repository and upload the contents of this folder (including `.github/workflows/build-windows.yml`).
2. Open the repository's **Actions** tab.
3. Select **Build VC Desk for Windows** and choose **Run workflow** (or push to `main`).
4. When the run finishes, open it and download the `VCDesk-Windows-x64` artifact.
5. Extract `VCDesk-Windows.zip`. It contains `VCDesk.exe` and `vcdesk-signaling.exe`.

The build uses a Windows GitHub runner. The target PC needs the Microsoft Edge WebView2 Runtime, which is already present on most supported Windows 10/11 systems. If it is missing, install Microsoft's WebView2 Runtime.

## Local development

Requires Go 1.22+, Wails v2.10.1, and the Windows build prerequisites described in the Wails documentation. Run `wails build` from this directory. Build the signaling server from `signaling` with `go build`.
