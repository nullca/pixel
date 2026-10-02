/* Midway Quest API client — drop into the game.
 * Rule: the game never writes coins/kp/items as rewards. It asks the server and shows what comes back.
 *
 * Token: the Midway portal (already logged in with KID) posts the access token into the game iframe:
 *   iframe.contentWindow.postMessage({type:'midway:token', token, exp}, GAME_ORIGIN)
 * and answers {type:'midway:token-request'} when the game needs a fresh one.
 */
export function createMidwayApi({ baseUrl, portalOrigin, onWallet, onInventory, onError }) {
  let token = null, exp = 0, waiters = [];

  addEventListener('message', (e) => {
    if (e.origin !== portalOrigin || !e.data || e.data.type !== 'midway:token') return;
    token = e.data.token; exp = e.data.exp || 0;
    waiters.splice(0).forEach((w) => w(token));
  });

  function getToken() {
    if (token && Date.now() / 1000 < exp - 30) return Promise.resolve(token);
    parent.postMessage({ type: 'midway:token-request' }, portalOrigin);
    return new Promise((res, rej) => { waiters.push(res); setTimeout(() => rej(new Error('ไม่ได้รับสิทธิ์จาก KID')), 8000); });
  }

  async function call(method, path, body, { idem = false } = {}) {
    const headers = { 'Content-Type': 'application/json', Authorization: 'Bearer ' + (await getToken()) };
    if (idem) headers['Idempotency-Key'] = typeof idem === 'string' ? idem : crypto.randomUUID();
    for (let attempt = 0; attempt < 3; attempt++) {
      let r;
      try {
        r = await fetch(baseUrl + path, { method, headers, body: body ? JSON.stringify(body) : undefined });
      } catch (err) {               // network: retry with the SAME idempotency key, so no double reward
        await new Promise((s) => setTimeout(s, 400 * (attempt + 1)));
        continue;
      }
      if (r.status === 401 && attempt === 0) { token = null; headers.Authorization = 'Bearer ' + (await getToken()); continue; }
      const data = await r.json().catch(() => ({}));
      if (!r.ok) { const e = new Error(data.error || r.statusText); e.status = r.status; onError && onError(e); throw e; }
      if (data.wallet && onWallet) onWallet(data.wallet);
      return data;
    }
    const e = new Error('เชื่อมต่อเซิร์ฟเวอร์ไม่ได้'); onError && onError(e); throw e;
  }

  // Each write gets its own key, created once per user action and reused on retry inside call().
  const w = (path, body) => call('POST', path, body, { idem: crypto.randomUUID() });

  return {
    me: () => call('GET', '/api/v1/me').then((d) => { onWallet && onWallet(d.wallet); onInventory && onInventory(d.inventory); return d; }),
    quizNext: (topic = '') => call('POST', '/api/v1/quiz/next', { topic }),
    quizAnswer: (token, choice) => w('/api/v1/quiz/answer', { token, choice }),
    startMinigame: (kind, spot) => call('POST', '/api/v1/minigame/start', { kind, spot }),
    finishMinigame: (session_id, result = {}) => w('/api/v1/minigame/finish', { session_id, ...result }),
    collect: (kind) => w('/api/v1/collect', { kind }),
    buy: (item_id, qty = 1) => w('/api/v1/shop/buy', { item_id, qty }),
    sell: (item_id, qty = 1) => w('/api/v1/shop/sell', { item_id, qty }),
    contribute: (amount) => w('/api/v1/team/contribute', { amount }),
    team: (dept) => call('GET', '/api/v1/team/' + encodeURIComponent(dept)),
  };
}

/* ---------------- How the game code changes (examples) ----------------

const API = createMidwayApi({
  baseUrl: 'https://kplay-api.midway.example.internal',
  portalOrigin: 'https://midway.example.internal',
  onWallet: (w) => { S.coins = w.coins; S.kp = w.kp; renderStats(); },   // display cache only
  onInventory: (inv) => { S.inv = inv; },
  onError: (e) => pop(e.message, '#F07A6A'),
});
await API.me();                                         // on boot, replaces localStorage balances

// Wind Run — before: S.coins += Math.max(15, 160 - tt)
async function startRace() { RACE = { ...RACE, srv: await API.startMinigame('run') }; }
async function finishRace() {
  const r = await API.finishMinigame(RACE.srv.session_id);
  banner(r.capped ? 'เข้าเส้นชัย (ครบโควตาเหรียญวันนี้)' : 'เข้าเส้นชัย!', (r.reward.coins || 0) + ' เหรียญ');
}

// Fishing — the server decides which fish you caught
const s = await API.startMinigame('fish', 'pond');      // when the bobber is cast
const r = await API.finishMinigame(s.session_id, { caught: true });
showCatch(r.reward.fish);                               // {id, size_cm, tier}

// Quiz — the client never sees the correct index before answering
const q = await API.quizNext('phishing');               // {token, prompt, choices}
const a = await API.quizAnswer(q.token, chosenIndex);   // {correct, answer, explain, reward}

// Team fund
const t = await API.contribute(500);                    // {total, level}
*/
