# VC Desk — Windows desktop voice & video

VC Desk is a Windows desktop application built with Go + Wails. The interface is embedded in the desktop app; it does not open a normal browser tab. The Windows build workflow compiles the application and its small signaling server on a hosted Windows runner, so the end user does not need Go, Node.js, or a compiler installed.

## Features

- Two-person voice and video rooms using WebRTC
- Camera and microphone permission handling
- Microphone mute/unmute and camera on/off
- Local and remote video preview
- Room codes and display names
- Leave-call control and connection status/error messages
- Automatically starts and stops its bundled local signaling server

## Running VC Desk

Extract the complete ZIP and keep both executables in the same folder:
- `VCDesk.exe`
- `vcdesk-signaling.exe`

Start `VCDesk.exe`. It automatically launches the bundled signaling server on `127.0.0.1:8080`, reuses an already-running local server if one is present, and stops the server it started when VC Desk closes. The local server binds to loopback only; it is not exposed to your LAN or the internet.

Enter a room code and display name, then click **Join voice & video**. Both people must use the same signaling server address and room code. For calls between different computers, deploy the signaling server on a public host with TLS and enter its secure `wss://.../ws` address. The bundled local server is for same-computer testing. Some restrictive networks also require a TURN relay; the client currently includes public STUN servers only.

The signaling server is a minimal prototype. Before public production deployment, add authentication, rate limiting, an explicit allowed-origin list, TLS termination, and a configured TURN service. Do not expose it publicly as-is for sensitive calls.

## Build without installing tools on the user's PC

1. Open the repository's **Actions** tab.
2. Select **Build VC Desk for Windows** and choose **Run workflow** (or push to `main`).
3. When the run finishes, open it and download the `VCDesk-Windows-x64` artifact.
4. Extract `VCDesk-Windows.zip`. It contains both executables listed above.

The build uses a Windows GitHub runner. The target PC needs the Microsoft Edge WebView2 Runtime, which is already present on most supported Windows 10/11 systems. If it is missing, install Microsoft's WebView2 Runtime.

## Local development

Requires Go 1.22+, Wails v2.10.1, and the Windows build prerequisites described in the Wails documentation. Run `wails build` from this directory. Build the signaling server from `signaling` with `go build`.
