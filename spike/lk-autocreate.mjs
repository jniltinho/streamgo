// What LiveKit (auto_create: false) does with a valid token for a room that does not exist, and a join racing DeleteRoom.
import crypto from 'node:crypto';
const KEY = 'spikekey', SECRET = 'spike-throwaway-secret-0123456789abcdef', HTTP = 'http://127.0.0.1:17880';
const b64 = (o) => Buffer.from(JSON.stringify(o)).toString('base64url');
const jwt = (sub, video) => { const n = Math.floor(Date.now() / 1000);
  const s = b64({ alg: 'HS256', typ: 'JWT' }) + '.' + b64({ iss: KEY, sub, nbf: n - 10, exp: n + 600, video });
  return s + '.' + crypto.createHmac('sha256', SECRET).update(s).digest('base64url'); };
const api = async (m, body) => (await fetch(`${HTTP}/twirp/livekit.RoomService/${m}`, { method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + jwt('admin', { roomCreate: true, roomList: true, roomAdmin: true, room: body.room || body.name }) },
  body: JSON.stringify(body) })).json();
const rooms = async () => (await api('ListRooms', {})).rooms?.map(r => r.name) || [];
const join = (room) => new Promise((res) => {
  const ws = new WebSocket(`ws://127.0.0.1:17880/rtc?access_token=${jwt('probe-' + Math.random(), { room, roomJoin: true })}&protocol=15&auto_subscribe=1`);
  const t = setTimeout(() => { ws.close(); res('timeout'); }, 4000);
  ws.onopen = () => {}; ws.onmessage = () => { clearTimeout(t); res('joined (got signal message)'); setTimeout(() => ws.close(), 300); };
  ws.onerror = () => { clearTimeout(t); res('ws error'); }; ws.onclose = (e) => { clearTimeout(t); res('closed ' + e.code); };
});
const v = await fetch(`${HTTP}/rtc/validate?access_token=${jwt('probe', { room: 'bc_nonexistent', roomJoin: true })}`);
console.log('validate bc_nonexistent:', v.status, (await v.text()).trim());
console.log('join bc_nonexistent:', await join('bc_nonexistent'), '| rooms now:', (await rooms()).filter(r => r.includes('nonexist')));
// race: create, then fire DeleteRoom and a join at the same time, several times
for (let i = 0; i < 5; i++) {
  const r = `bc_race${i}`; await api('CreateRoom', { name: r });
  const [d, j] = await Promise.all([api('DeleteRoom', { room: r }), join(r)]);
  await new Promise(s => setTimeout(s, 500));
  console.log(`race ${i}: delete=${JSON.stringify(d)} join=${j} room exists after=${(await rooms()).includes(r)}`);
}
