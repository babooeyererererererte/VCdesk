'use strict';

const $ = (id) => document.getElementById(id);
const state = {
  ws: null,
  pc: null,
  stream: null,
  room: '',
  name: '',
  micOn: true,
  camOn: true,
  isInitiator: false,
  makingOffer: false,
  ignoreOffer: false,
  pendingCandidates: [],
  iceServers: [{ urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] }],
  turnAvailable: false,
};

function setStatus(message, kind = '') {
  const el = $('status');
  el.textContent = `● ${message}`;
  el.dataset.state = kind;
}

function setCallInfo(message) { $('callInfo').textContent = message; }
function send(payload) {
  if (state.ws && state.ws.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify({ ...payload, room: state.room, name: state.name }));
  }
}

function setControls(inCall) {
  $('joinBtn').disabled = inCall;
  $('hangBtn').disabled = !inCall;
  $('micBtn').disabled = !inCall;
  $('camBtn').disabled = !inCall;
}

async function loadIceServers() {
  try {
    const configuredServer = new URL($('server').value.trim());
    const apiBase = (configuredServer.protocol === 'wss:' ? 'https:' : 'http:') + '//' + configuredServer.host;
    const response = await fetch(apiBase + '/turn-credentials', { cache: 'no-store' });
    if (!response.ok) throw new Error('TURN service HTTP ' + response.status);
    const data = await response.json();
    if (!Array.isArray(data.iceServers) || !data.iceServers.length) throw new Error('No ICE servers returned');
    state.iceServers = data.iceServers; state.turnAvailable = data.turnAvailable === true; return true;
  } catch (error) {
    state.iceServers = [{ urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] }];
    state.turnAvailable = false; console.warn('TURN unavailable:', error);
    setCallInfo('TURN relay unavailable. Direct connection only; different networks may fail.'); return false;
  }
}

async function startMedia() {
  if (!navigator.mediaDevices?.getUserMedia) {
    throw new Error('Camera access is unavailable. Check that the app is running in a supported Windows environment.');
  }
  state.stream = await navigator.mediaDevices.getUserMedia({
    audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    video: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30, max: 30 } },
  });
  $('localVideo').srcObject = state.stream;
  $('localVideo').play().catch(() => {});
}

function makePeer(initiator) {
  if (state.pc) state.pc.close();
  state.isInitiator = initiator;
  state.pendingCandidates = [];
  const pc = new RTCPeerConnection({ iceServers: state.iceServers, iceCandidatePoolSize: 2 });
  state.pc = pc;
  for (const track of state.stream.getTracks()) pc.addTrack(track, state.stream);

  pc.ontrack = (event) => {
    const remote = $('remoteVideo');
    if (event.streams && event.streams[0]) remote.srcObject = event.streams[0];
    $('remoteLabel').textContent = 'Connected participant';
    remote.play().catch(() => {});
    setCallInfo('Connected — voice and video are live.');
  };
  pc.onicecandidate = (event) => {
    if (event.candidate) send({ type: 'candidate', candidate: event.candidate });
  };
  pc.onconnectionstatechange = () => {
    if (pc !== state.pc) return;
    const connectionState = pc.connectionState;
    if (connectionState === 'connected') setStatus(`Connected to ${state.room}`, 'connected');
    else if (connectionState === 'connecting') setStatus('Connecting…');
    else if (connectionState === 'disconnected') setStatus('Connection interrupted');
    else if (connectionState === 'failed') {
      setStatus('Connection failed', 'error');
      setCallInfo(state.turnAvailable ? 'Connection failed even with TURN. Check firewall restrictions.' : 'Connection failed: TURN is not configured on the server.');
    }
  };
  pc.oniceconnectionstatechange = () => {
    if (pc.iceConnectionState === 'failed') {
      setCallInfo(state.turnAvailable ? 'ICE failed despite TURN credentials. Check network restrictions.' : 'ICE failed: no TURN relay is configured.');
    }
  };
  pc.onnegotiationneeded = async () => {
    if (!state.isInitiator || pc !== state.pc) return;
    try {
      state.makingOffer = true;
      await pc.setLocalDescription();
      send({ type: 'description', description: pc.localDescription });
    } catch (error) {
      console.error('Offer creation failed:', error);
      setCallInfo(`Could not start the call: ${error.message}`);
    } finally {
      state.makingOffer = false;
    }
  };
  return pc;
}

async function handleSignal(message) {
  if (message.type === 'peer-joined') {
    $('remoteLabel').textContent = `${message.name || 'Someone'} is joining…`;
    if (!state.pc) makePeer(true);
    return;
  }
  if (message.type === 'room-full') {
    setCallInfo('That room is full. This version supports two people per room.');
    leaveCall(true);
    return;
  }
  if (message.type === 'peer-left') {
    if (state.pc) { state.pc.close(); state.pc = null; }
    $('remoteVideo').srcObject = null;
    $('remoteLabel').textContent = 'Participant left';
    setCallInfo('The other person left. Waiting for someone else…');
    return;
  }
  if (message.type === 'server-error') {
    setCallInfo(message.message || 'The signaling server reported an error.');
    return;
  }
  if (message.type === 'description') {
    if (!state.pc) makePeer(false);
    const pc = state.pc;
    const description = message.description;
    const offerCollision = description.type === 'offer' &&
      (state.makingOffer || pc.signalingState !== 'stable');
    state.ignoreOffer = !state.isInitiator && offerCollision;
    if (state.ignoreOffer) return;
    try {
      await pc.setRemoteDescription(description);
      while (state.pendingCandidates.length) {
        await pc.addIceCandidate(state.pendingCandidates.shift());
      }
      if (description.type === 'offer') {
        await pc.setLocalDescription();
        send({ type: 'description', description: pc.localDescription });
      }
    } catch (error) {
      console.error('Description handling failed:', error);
      setCallInfo(`Call setup error: ${error.message}`);
    }
    return;
  }
  if (message.type === 'candidate' && message.candidate) {
    if (!state.pc || !state.pc.remoteDescription) {
      state.pendingCandidates.push(message.candidate);
    } else {
      try { await state.pc.addIceCandidate(message.candidate); }
      catch (error) { if (!state.ignoreOffer) console.warn('ICE candidate rejected:', error); }
    }
  }
}

async function joinCall() {
  if (state.ws) return;
  state.room = $('room').value.trim();
  state.name = $('name').value.trim() || 'Guest';
  const serverURL = $('server').value.trim();
  if (!/^[a-zA-Z0-9_-]{2,32}$/.test(state.room)) {
    alert('Room codes must be 2–32 characters and use only letters, numbers, hyphens, or underscores.');
    return;
  }
  let parsed;
  try { parsed = new URL(serverURL); }
  catch { alert('Enter a valid signaling server address.'); return; }
  if (!['ws:', 'wss:'].includes(parsed.protocol)) {
    alert('The signaling address must start with ws:// or wss://.'); return;
  }

  $('joinBtn').disabled = true;
  setCallInfo('Requesting camera and microphone permission…');
  try {
    await loadIceServers();
    await startMedia();
    state.ws = new WebSocket(serverURL);
  } catch (error) {
    cleanupMedia();
    $('joinBtn').disabled = false;
    setCallInfo('Could not access camera or microphone.');
    alert(`Unable to start media: ${error.message}`);
    return;
  }

  state.ws.onopen = () => {
    send({ type: 'join' });
    setStatus(`In room ${state.room}`);
    setCallInfo('Joined room. Waiting for another participant…');
    setControls(true);
  };
  state.ws.onmessage = async (event) => {
    try { await handleSignal(JSON.parse(event.data)); }
    catch (error) { console.error('Invalid signaling message:', error); }
  };
  state.ws.onerror = () => {
    setStatus('Signaling server error', 'error');
    setCallInfo('Could not reach the signaling server. Check the address and server status.');
  };
  state.ws.onclose = () => {
    state.ws = null;
    if (state.stream || state.pc) leaveCall(false);
    setStatus('Not connected');
  };
}

function cleanupMedia() {
  if (state.stream) {
    for (const track of state.stream.getTracks()) track.stop();
    state.stream = null;
  }
  $('localVideo').srcObject = null;
  $('remoteVideo').srcObject = null;
}

function leaveCall(closeSocket = true) {
  if (state.pc) { state.pc.ontrack = null; state.pc.close(); state.pc = null; }
  cleanupMedia();
  if (closeSocket && state.ws) {
    const socket = state.ws;
    state.ws = null;
    socket.onclose = null;
    socket.close(1000, 'User left call');
  }
  state.isInitiator = false;
  state.pendingCandidates = [];
  state.micOn = true;
  state.camOn = true;
  $('micBtn').textContent = '🎙 Mic on';
  $('camBtn').textContent = '📷 Camera on';
  $('remoteLabel').textContent = 'Waiting for someone…';
  setControls(false);
  setCallInfo('Not in a call');
  setStatus('Not connected');
}

$('joinBtn').addEventListener('click', joinCall);
$('micBtn').addEventListener('click', () => {
  if (!state.stream) return;
  state.micOn = !state.micOn;
  state.stream.getAudioTracks().forEach(track => { track.enabled = state.micOn; });
  $('micBtn').textContent = state.micOn ? '🎙 Mic on' : '🔇 Mic off';
});
$('camBtn').addEventListener('click', () => {
  if (!state.stream) return;
  state.camOn = !state.camOn;
  state.stream.getVideoTracks().forEach(track => { track.enabled = state.camOn; });
  $('camBtn').textContent = state.camOn ? '📷 Camera on' : '🚫 Camera off';
});
$('hangBtn').addEventListener('click', () => leaveCall(true));
window.addEventListener('beforeunload', () => leaveCall(true));
setControls(false);
