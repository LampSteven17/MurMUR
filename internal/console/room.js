// room.js — the server room, rendered as pixel art, driven by the real event
// stream. Fred walks to the rack he is actually working on; rack LEDs are live
// guest status. Nothing here is decorative-only: every pixel is bound to data.
"use strict";
(() => {
  const W = 400, H = 200;           // logical pixels; canvas scales up integer-only
  const FLOOR_Y = 150;              // where Fred's feet land
  const cvs = document.getElementById("room");
  if (!cvs) return;
  const ctx = cvs.getContext("2d");
  ctx.imageSmoothingEnabled = false;

  // ---- palette (warm cozy dark, Stardew-adjacent) --------------------------
  const P = {
    wall:"#1b2430", wallLit:"#243040", floor:"#2a2118", floorDark:"#221b14",
    rack:"#39424f", rackDark:"#2b323c", rackLight:"#4a5566", vent:"#1d232c",
    led_on:"#3fb950", led_warn:"#B78E11", led_off:"#5a5a5a", led_err:"#C43D3D",
    hair:"#6b4423", skin:"#f0c8a0", eye:"#1a1f28", shirt:"#3E86CC",
    shirtD:"#2d6199", pants:"#3a4a5a", boot:"#4a3728", tool:"#c9d1d9",
    clip:"#d8c9a0", glow:"#58a6ff", cable:"#1a2028",
  };

  // ---- sprites: each row is a string, one char per pixel ------------------
  const SPR = {
    fred_idle: [
      "....hhhh....",
      "...hhhhhh...",
      "..hssssssh..",
      "..hsesesssh.",
      "..hssssssh..",
      "...ssssss...",
      "..CCCCCCCC..",
      ".sCCCCCCCCs.",
      ".sCCCCCCCCs.",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "...pppppp...",
      "...pp..pp...",
      "...pp..pp...",
      "...bb..bb...",
      "...bb..bb...",
    ],
    fred_walk: [
      "....hhhh....",
      "...hhhhhh...",
      "..hssssssh..",
      "..hsesesssh.",
      "..hssssssh..",
      "...ssssss...",
      "..CCCCCCCC..",
      ".sCCCCCCCCs.",
      ".sCCCCCCCCs.",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "...pppppp...",
      "..pp....pp..",
      "..pp....pp..",
      ".bb......bb.",
      ".bb......bb.",
    ],
    fred_check: [   // clipboard raised, reading
      "....hhhh....",
      "...hhhhhh...",
      "..hssssssh..",
      "..hsesesssh.",
      "..hssssssh..",
      "...ssssss...",
      "..CCCCCCCC..",
      ".sCCCCCCCC..",
      "kkCCCCCCCC..",
      "kkCCCCCCCC..",
      "..CCCCCCCC..",
      "...pppppp...",
      "...pp..pp...",
      "...pp..pp...",
      "...bb..bb...",
      "...bb..bb...",
    ],
    fred_work: [    // arm out with tool, working on a rack
      "....hhhh....",
      "...hhhhhh...",
      "..hssssssh..",
      "..hsesesssh.",
      "..hssssssh..",
      "...ssssss...",
      "..CCCCCCCC..",
      ".sCCCCCCCCst",
      ".sCCCCCCCCs.",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "...pppppp...",
      "...pp..pp...",
      "...pp..pp...",
      "...bb..bb...",
      "...bb..bb...",
    ],
    fred_alert: [   // both arms up — an escalation is waiting
      "s..hhhh...s.",
      "s.hhhhhh..s.",
      "shssssssh.s.",
      ".hsesesssh..",
      "..hssssssh..",
      "...ssssss...",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "..CCCCCCCC..",
      "...pppppp...",
      "...pp..pp...",
      "...pp..pp...",
      "...bb..bb...",
      "...bb..bb...",
    ],
  };
  const CHAR = {h:P.hair, s:P.skin, e:P.eye, C:P.shirt, c:P.shirtD, p:P.pants,
                b:P.boot, t:P.tool, k:P.clip};

  function blit(spr, x, y, flip) {
    for (let r = 0; r < spr.length; r++) {
      const row = spr[r];
      for (let c = 0; c < row.length; c++) {
        const col = CHAR[row[c]];
        if (!col) continue;
        ctx.fillStyle = col;
        ctx.fillRect(Math.round(x) + (flip ? row.length - 1 - c : c), y + r, 1, 1);
      }
    }
  }

  // ---- world state --------------------------------------------------------
  let racks = [];            // {node, x, guests:[{name,status}]}
  let fred = { x: 40, target: 40, action: "idle", say: "booting up…", frame: 0, flip: false };
  let unacked = 0, tick = 0;

  function layoutRacks(nodes) {
    const n = Math.max(nodes.length, 1);
    const gap = Math.floor((W - 40) / n);
    racks = nodes.map((node, i) => ({ node, x: 24 + i * gap, guests: [] }));
  }

  function drawRoom() {
    // wall + a soft light pool behind the racks
    ctx.fillStyle = P.wall; ctx.fillRect(0, 0, W, FLOOR_Y);
    ctx.fillStyle = P.wallLit; ctx.fillRect(0, 30, W, 60);
    // cable tray along the top
    ctx.fillStyle = P.cable; ctx.fillRect(0, 12, W, 6);
    for (let x = 4; x < W; x += 14) { ctx.fillStyle = P.rackDark; ctx.fillRect(x, 18, 2, 4); }
    // floor
    ctx.fillStyle = P.floor; ctx.fillRect(0, FLOOR_Y, W, H - FLOOR_Y);
    ctx.fillStyle = P.floorDark;
    for (let x = 0; x < W; x += 16) ctx.fillRect(x, FLOOR_Y, 1, H - FLOOR_Y);
    ctx.fillRect(0, FLOOR_Y, W, 1);
  }

  function drawRack(r) {
    const x = r.x, y = 52, w = 22, h = 96;
    ctx.fillStyle = P.rackDark; ctx.fillRect(x - 1, y - 1, w + 2, h + 2);
    ctx.fillStyle = P.rack; ctx.fillRect(x, y, w, h);
    ctx.fillStyle = P.rackLight; ctx.fillRect(x, y, w, 1);
    // guest slots — one per guest on this node, with a live status LED
    r.guests.slice(0, 7).forEach((g, i) => {
      const sy = y + 6 + i * 12;
      ctx.fillStyle = P.vent; ctx.fillRect(x + 2, sy, w - 4, 9);
      for (let v = 0; v < 4; v++) { ctx.fillStyle = P.rackDark; ctx.fillRect(x + 5 + v * 3, sy + 3, 2, 4); }
      const lit = g.status === "running";
      const blink = lit && ((tick >> 4) + i) % 7 === 0;
      ctx.fillStyle = !lit ? P.led_off : (blink ? "#a5f3b0" : P.led_on);
      ctx.fillRect(x + w - 5, sy + 3, 2, 2);
    });
    // empty node: a single dim standby light
    if (r.guests.length === 0) {
      ctx.fillStyle = P.led_off; ctx.fillRect(x + w - 5, y + 9, 2, 2);
    }
  }

  function drawLabel(text, x, y, color) {
    ctx.font = "8px ui-monospace, monospace";
    ctx.textAlign = "center";
    ctx.fillStyle = color;
    ctx.fillText(text, x, y);
    ctx.textAlign = "left";
  }

  function drawBubble(text, x, y) {
    ctx.font = "8px ui-monospace, monospace";
    const w = Math.min(ctx.measureText(text).width + 8, 190);
    const bx = Math.max(2, Math.min(W - w - 2, x - w / 2)), by = y - 16;
    ctx.fillStyle = "#0d1117"; ctx.fillRect(bx - 1, by - 1, w + 2, 13);
    ctx.fillStyle = "#161b22"; ctx.fillRect(bx, by, w, 11);
    ctx.fillStyle = "#161b22"; ctx.fillRect(x - 2, by + 11, 4, 3);
    ctx.fillStyle = "#c9d1d9"; ctx.textAlign = "left";
    ctx.fillText(text, bx + 4, by + 8, w - 8);
  }

  function frame() {
    tick++;
    ctx.clearRect(0, 0, W, H);
    drawRoom();
    racks.forEach(r => {
      drawRack(r);
      drawLabel(r.node.replace("prxy-", ""), r.x + 11, 46, "#6e7681");
    });

    // walk toward the target rack
    const dx = fred.target - fred.x;
    if (Math.abs(dx) > 1.2) {
      fred.flip = dx < 0;
      fred.x += Math.sign(dx) * 0.9;
      fred.frame++;
      blit((fred.frame >> 3) % 2 ? SPR.fred_walk : SPR.fred_idle, fred.x, FLOOR_Y - 16, fred.flip);
    } else {
      const spr = fred.action === "work" ? SPR.fred_work
                : fred.action === "check" ? SPR.fred_check
                : fred.action === "alert" ? SPR.fred_alert
                : SPR.fred_idle;
      // little bob so he never looks frozen
      const bob = (fred.action === "idle" && (tick >> 5) % 2) ? 1 : 0;
      blit(spr, fred.x, FLOOR_Y - 16 + bob, fred.flip);
    }
    drawBubble(fred.say, fred.x + 6, FLOOR_Y - 18);

    if (unacked > 0) {
      ctx.fillStyle = P.led_err;
      ctx.fillRect(W - 60, 6, 54, 11);
      ctx.fillStyle = "#0d1117"; ctx.font = "8px ui-monospace, monospace";
      ctx.fillText(unacked + " to ack", W - 56, 15);
    }
    requestAnimationFrame(frame);
  }

  // ---- bind Fred's behavior to real events --------------------------------
  function rackFor(subject) {
    if (!subject) return null;
    for (const r of racks) {
      if (r.node === subject || r.node.endsWith(subject)) return r;
      if (r.guests.some(g => g.name === subject)) return r;
    }
    return null;
  }

  function react(e) {
    if (!e) return;
    const sub = e.subject || "";
    const r = rackFor(sub);
    if (r) fred.target = r.x - 14;

    if (e.severity === "escalate") {
      fred.action = "alert"; fred.say = "! " + (sub || "problem") + " needs you";
      return;
    }
    switch (e.kind) {
      case "update":
        fred.action = "work";
        fred.say = "updating " + (sub || "an app") + "…";
        break;
      case "patrol":
        fred.action = "check";
        fred.say = (e.message || "patrolling").slice(0, 40);
        break;
      case "finding":
        fred.action = "check";
        fred.say = (sub ? sub + ": " : "") + (e.message || "").slice(0, 34);
        break;
      case "backup":
        fred.action = "work"; fred.say = "checking backups…";
        break;
      default:
        fred.action = "idle";
        fred.say = (e.message || "all quiet").slice(0, 40);
    }
    if (!r) fred.target = 40 + ((tick * 7) % (W - 100));  // wander if unmapped
  }

  async function load() {
    try {
      const f = await (await fetch("/api/fleet")).json();
      layoutRacks(f.nodes && f.nodes.length ? f.nodes : [...new Set((f.guests || []).map(g => g.node))]);
      (f.guests || []).forEach(g => {
        const r = racks.find(r => r.node === g.node);
        if (r) r.guests.push({ name: g.name, status: g.status });
      });
    } catch { layoutRacks(["node"]); }

    try {
      const d = await (await fetch("/api/events?limit=60")).json();
      const evs = d.events || [];
      const acked = new Set(evs.filter(e => e.kind === "ack").map(e => e.ref));
      unacked = evs.filter(e => e.severity === "escalate" && !acked.has(e.id)).length;
      const last = [...evs].reverse().find(e => e.kind !== "ack");
      react(last);
    } catch {}
  }

  const es = new EventSource("/api/stream");
  es.onmessage = m => { try { const e = JSON.parse(m.data); if (e.kind !== "ack") react(e); else unacked = Math.max(0, unacked - 1); } catch {} };

  load().then(() => requestAnimationFrame(frame));
  setInterval(load, 60000);   // refresh rack state; events keep Fred current
})();
