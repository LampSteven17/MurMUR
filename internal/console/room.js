// room.js — the server room as a 3/4 top-down pixel-art scene, driven entirely
// by the event spine. Fred walks the aisles to whatever rack the stream says he
// is working on, and sleeps in the back room when the cluster is quiet.
//
// Nothing here is decorative-only: racks are real cluster nodes, the lights on
// them are real guest status, and his destination, pose and dialogue come from
// live events.
//
// Projection: the floor is drawn unforeshortened (a 16px tile is 16px on both
// axes) while objects are drawn as near-elevation with a short top face. That
// is geometrically impossible, and is exactly what Zelda and Chrono Trigger do
// — you identify a rack by its FRONT (slots, vents, lights), never by its top,
// so a true overhead camera would render this as a floor plan instead of a room.
"use strict";
(() => {
  const cvs = document.getElementById("room");
  if (!cvs) return;

  const W = 240, H = 160, TILE = 16;
  cvs.width = W; cvs.height = H;
  const ctx = cvs.getContext("2d", {alpha: false});
  ctx.imageSmoothingEnabled = false;

  const A = window.anime || null;   // optional: scene still runs without it

  // --- palette -------------------------------------------------------------
  // Twelve muted cool structural colours; saturated colour is reserved for
  // emissives, so the status lights are the only vivid pixels in the frame.
  // Fred is warm, which separates him from the room without competing.
  const C = {
    ink:"#0f0f18", deep:"#181425", wall:"#262b44", wallCap:"#3a4466",
    floorA:"#2b3350", floorB:"#262d48", seam:"#222840", scuff:"#313a5c",
    rackEdge:"#141020", rackTop:"#5a6988", rackFace:"#3a4466",
    rackLit:"#4a5878", vent:"#1d2338",
    bedFrame:"#5a4632", bedTop:"#6f5740", quilt:"#8b9bb4", pillow:"#c0cbdc",
    rug:"#33405e",
    on:"#63c74d", bad:"#e43b44", off:"#2f3854",
    hair:"#4a3526", skin:"#eec39a", shirt:"#96603c", pants:"#39405e",
    boot:"#181425", dark:"#141020", txt:"#c0cbdc", dim:"#5a6988",
  };

  // --- character sprites: 12 wide, 18 tall, one char per pixel -------------
  // Two leg positions per direction. The stride is carried by swapping the legs
  // AND lifting the whole sprite 1px — animating legs alone reads as shuffling.
  const SPR = {
    down: [
      "....hhhh....", "..hhhhhhhh..", "..hhhhhhhh..", "..hssssssh..",
      "..hsEssEsh..", "..hssssssh..", "...ssssss...", "..kCCCCCCk..",
      ".kCCCCCCCCk.", ".sCCCCCCCCs.", ".sCCCCCCCCs.", ".kCCCCCCCCk.",
      "..CCCCCCCC..", "..PPPPPPPP..", "..PPP..PPP..", "..PPP..PPP..",
      "..bb....bb..", "..bb....bb..",
    ],
    up: [
      "....hhhh....", "..hhhhhhhh..", "..hhhhhhhh..", "..hhhhhhhh..",
      "..hhhhhhhh..", "..hhhhhhhh..", "...hhhhhh...", "..kCCCCCCk..",
      ".kCCCCCCCCk.", ".sCCCCCCCCs.", ".sCCCCCCCCs.", ".kCCCCCCCCk.",
      "..CCCCCCCC..", "..PPPPPPPP..", "..PPP..PPP..", "..PPP..PPP..",
      "..bb....bb..", "..bb....bb..",
    ],
    side: [
      "...hhhh.....", "..hhhhhh....", "..hhhhhhh...", "..hhssssh...",
      "..hhsEsss...", "..hhssssh...", "...ssssh....", "..kCCCCk....",
      "..kCCCCCk...", "..CCCCCCs...", "..CCCCCCs...", "..kCCCCk....",
      "..CCCCCC....", "..PPPPPP....", "..PPPP......", "..PPPP......",
      "..bbb.......", "..bbb.......",
    ],
  };
  // Legs-apart variant: only the final four rows differ.
  const LEGS_APART = {
    down: ["..PPP..PPP..", ".PPP....PPP.", ".bb......bb.", ".bb......bb."],
    up:   ["..PPP..PPP..", ".PPP....PPP.", ".bb......bb.", ".bb......bb."],
    side: ["..PPPP......", ".PPPPP......", ".bb..bb.....", ".bb..bb....."],
  };
  // Asleep is a dedicated pose, never a rotated walk frame. 20x9, head at left.
  const SLEEP = [
    ".....hhhh...........", "....hssssh..........", "....hs--sh..........",
    "....hsssss..........", ".....ssss...........", "...QQQQQQQQQQQQQ....",
    "..QQQQQQQQQQQQQQQ...", "..QQQQQQQQQQQQQQQ...", "...QQQQQQQQQQQQQ....",
  ];
  const PX = {h:C.hair, s:C.skin, E:C.ink, C:C.shirt, P:C.pants, b:C.boot,
              k:C.dark, Q:C.quilt, "-":C.ink};

  function blit(rows, x, y, flip) {
    x = Math.round(x); y = Math.round(y);
    const w = rows[0].length;
    for (let r = 0; r < rows.length; r++) {
      const row = rows[r];
      for (let c = 0; c < row.length; c++) {
        const col = PX[row[c]];
        if (!col) continue;
        ctx.fillStyle = col;
        ctx.fillRect(x + (flip ? w - 1 - c : c), y + r, 1, 1);
      }
    }
  }

  // --- room geometry -------------------------------------------------------
  const WALL_H = 26, FLOOR_B = 154;
  const BUNK = {x:6, y:30, w:58, h:120};
  const BED  = {x:14, y:44, w:28, h:38};
  const DOOR = {y0:126, y1:150};              // gap in the bunk's right wall
  const RACK_W = 16, RACK_H = 28, TOP_H = 5;  // 5px top face, 23px front face
  const ROW_A_Y = 72, ROW_B_Y = 128;          // baseline = front-most floor row
  const RACK_X = [88, 124, 160, 196];
  const AISLE_MAIN = 94, AISLE_FRONT = 148;   // the two lines Fred walks
  const GAPS = [79, 114, 150, 186, 222];      // connectors between the rows
  const DOOR_X = BUNK.x + BUNK.w + 10;

  let racks = [];   // {node, x, base, guests:[{name,status}]}
  const leds = [];

  function layout(nodes) {
    racks = nodes.slice(0, 8).map((node, i) => ({
      node, x: RACK_X[i % 4], base: i < 4 ? ROW_A_Y : ROW_B_Y, guests: [],
    }));
  }

  function rebuildLeds() {
    leds.length = 0;
    // Non-harmonic periods: with shared factors the whole room visibly blinks in
    // unison within seconds and reads as one machine instead of eight.
    const PERIODS = [370, 530, 790, 1130, 1490];
    racks.forEach((r, ri) => {
      const top = r.base - RACK_H + TOP_H;
      for (let s = 0; s < 6; s++) {
        const g = r.guests[s];
        leds.push({
          x: r.x + RACK_W - 3, y: top + 2 + s * 3,
          color: !g ? C.off : g.status === "running" ? C.on : C.bad,
          period: PERIODS[(ri + s) % PERIODS.length],
          phase: (ri * 137 + s * 61) % 1000,
          mode: !g ? "off" : (s % 3 === 0 ? "steady" : "act"),
        });
      }
    });
  }

  function hash2(x, y) {
    let h = x * 374761393 + y * 668265263;
    h = (h ^ (h >> 13)) * 1274126177;
    return ((h ^ (h >> 16)) >>> 0) / 4294967296;
  }

  // --- static painting -----------------------------------------------------
  let bg = null;       // floor and walls never change: paint once, then blit
  let TERMINAL = null; // the bunk monitor, lit at draw time
  function paintBackground() {
    bg = document.createElement("canvas");
    bg.width = W; bg.height = H;
    const b = bg.getContext("2d");
    b.fillStyle = C.deep; b.fillRect(0, 0, W, H);

    b.fillStyle = C.wall;    b.fillRect(0, 0, W, WALL_H);
    b.fillStyle = C.wallCap; b.fillRect(0, WALL_H - 2, W, 2);
    b.fillStyle = C.deep;
    for (let x = 8; x < W; x += 24) b.fillRect(x, 4, 1, WALL_H - 8);

    // Floor on a two-shade checker with sparse scuffs, so the eye has something
    // to latch onto other than the grid.
    for (let y = WALL_H; y < FLOOR_B; y += TILE) {
      for (let x = 0; x < W; x += TILE) {
        b.fillStyle = ((x / TILE + y / TILE) & 1) ? C.floorA : C.floorB;
        b.fillRect(x, y, TILE, TILE);
        const r = hash2(x, y);
        if (r > 0.86) {
          b.fillStyle = C.scuff;
          b.fillRect(x + 3 + ((r * 9) | 0), y + 5 + ((r * 7) | 0), 2, 1);
        }
      }
    }
    // Raised-floor panels are coarser than the tile grid; the heavier seam every
    // two tiles gives a 32px rhythm so the 16px grid stops dominating.
    b.fillStyle = C.seam;
    for (let y = WALL_H; y <= FLOOR_B; y += TILE) b.fillRect(0, y, W, 1);
    for (let x = 0; x <= W; x += TILE) b.fillRect(x, WALL_H, 1, FLOOR_B - WALL_H);
    b.fillStyle = C.deep;
    for (let y = WALL_H; y <= FLOOR_B; y += TILE * 2) b.fillRect(0, y, W, 1);
    b.fillRect(0, FLOOR_B, W, H - FLOOR_B);

    // bunk room: rug, partition wall broken by a doorway onto the front aisle
    b.fillStyle = C.rug; b.fillRect(BUNK.x, BUNK.y, BUNK.w, BUNK.h);
    b.fillStyle = C.wall;
    b.fillRect(BUNK.x + BUNK.w, BUNK.y - 4, 3, DOOR.y0 - BUNK.y + 4);
    b.fillRect(BUNK.x + BUNK.w, DOOR.y1, 3, FLOOR_B - DOOR.y1);
    b.fillRect(BUNK.x - 3, BUNK.y - 4, 3, BUNK.h + 4);
    b.fillStyle = C.wallCap; b.fillRect(BUNK.x + BUNK.w, BUNK.y - 4, 3, 2);

    // bed, in the same 3/4 as everything else: top face then the front rail
    b.fillStyle = C.rackEdge; b.fillRect(BED.x - 1, BED.y - 1, BED.w + 2, BED.h + 2);
    b.fillStyle = C.bedFrame; b.fillRect(BED.x, BED.y, BED.w, BED.h);
    b.fillStyle = C.bedTop;   b.fillRect(BED.x + 1, BED.y + 1, BED.w - 2, BED.h - 6);
    b.fillStyle = C.pillow;   b.fillRect(BED.x + 3, BED.y + 3, BED.w - 6, 7);

    // desk with a terminal, so the bunk reads as somewhere lived-in rather than
    // a blank slab. The screen is an emissive and gets its glow at draw time.
    const D = {x: BUNK.x + 8, y: BUNK.y + 66, w: 34, h: 16};
    b.fillStyle = C.rackEdge; b.fillRect(D.x - 1, D.y - 1, D.w + 2, D.h + 2);
    b.fillStyle = C.rackFace; b.fillRect(D.x, D.y, D.w, D.h);
    b.fillStyle = C.rackTop;  b.fillRect(D.x, D.y, D.w, 4);
    b.fillStyle = C.rackEdge; b.fillRect(D.x + 6, D.y - 8, 14, 9);   // monitor body
    b.fillStyle = C.vent;     b.fillRect(D.x + 7, D.y - 7, 12, 7);
    b.fillStyle = C.rackEdge; b.fillRect(D.x + 24, D.y + 6, 4, 4);   // mug
    b.fillStyle = C.bedFrame; b.fillRect(D.x + 25, D.y + 7, 2, 2);
    // a chair, pushed in
    b.fillStyle = C.rackEdge; b.fillRect(D.x + 12, D.y + 18, 10, 8);
    b.fillStyle = C.rug;      b.fillRect(D.x + 13, D.y + 19, 8, 6);

    b.fillStyle = C.dim; b.font = "5px ui-monospace, monospace"; b.textAlign = "left";
    b.fillText("bunk", BUNK.x + 4, BUNK.y + BUNK.h - 5);

    // Floor light pools under the ceiling fixtures, built from nested additive
    // rects. A dim room with a few pools of light hides more tile repetition
    // than any amount of tile variation, and it costs almost nothing.
    // Ellipses drawn one scanline at a time. Nested hard rectangles read as
    // grey slabs, not light; a per-row half-width gives a real pool while
    // staying on the pixel grid.
    b.globalCompositeOperation = "lighter";
    b.fillStyle = "#a8bce6";
    const pool = (cx, cy, rx, ry) => {
      for (let dy = -ry; dy <= ry; dy++) {
        const k = 1 - (dy * dy) / (ry * ry);
        if (k <= 0) continue;
        const hw = Math.round(rx * Math.sqrt(k));
        b.globalAlpha = 0.075 * k * k;          // squared falloff: bright core, soft edge
        b.fillRect(Math.round(cx - hw), Math.round(cy + dy), hw * 2, 1);
      }
    };
    pool(122, AISLE_MAIN - 4, 54, 15);
    pool(196, AISLE_MAIN - 4, 44, 13);
    pool(150, AISLE_FRONT - 2, 60, 14);
    pool(BUNK.x + 26, BUNK.y + 78, 26, 12);
    b.globalAlpha = 1; b.globalCompositeOperation = "source-over";
    TERMINAL = {x: D.x + 7, y: D.y - 7, w: 12, h: 7};
  }

  function drawRack(r) {
    const top = r.base - RACK_H, x = r.x;
    ctx.fillStyle = C.rackEdge; ctx.fillRect(x - 1, top - 1, RACK_W + 2, RACK_H + 2);
    ctx.fillStyle = C.rackTop;  ctx.fillRect(x, top, RACK_W, TOP_H);
    ctx.fillStyle = C.rackFace; ctx.fillRect(x, top + TOP_H, RACK_W, RACK_H - TOP_H);
    ctx.fillStyle = C.rackLit;  ctx.fillRect(x, top + TOP_H, 2, RACK_H - TOP_H);
    ctx.fillStyle = C.vent;
    for (let s = 0; s < 6; s++) ctx.fillRect(x + 3, top + TOP_H + 1 + s * 3, RACK_W - 7, 2);
    ctx.fillStyle = C.rackEdge; ctx.fillRect(x, r.base - 2, RACK_W, 2);
    ctx.fillStyle = C.dim; ctx.font = "5px ui-monospace, monospace"; ctx.textAlign = "center";
    ctx.fillText(r.node.replace("prxy-", ""), x + RACK_W / 2, top - 2);
    ctx.textAlign = "left";
  }

  // --- Fred ----------------------------------------------------------------
  const fred = {
    x: BED.x + 8, y: BED.y + 30, dir: "down", flip: false,
    dist: 0, asleep: true, working: false, say: "", sayUntil: 0,
    lastEvent: Date.now(),
  };
  let unacked = 0, tl = null, walking = false;

  function inBunk() { return fred.x < BUNK.x + BUNK.w + 4; }

  function pathTo(tx, ty) {
    // Route along the aisles instead of straight-lining through hardware.
    const pts = [];
    let cx = fred.x, cy = fred.y;
    if (inBunk()) {
      if (Math.abs(cy - AISLE_FRONT) > 3) pts.push({x: cx, y: AISLE_FRONT});
      pts.push({x: DOOR_X, y: AISLE_FRONT});
      cx = DOOR_X; cy = AISLE_FRONT;
    } else if (Math.abs(cy - AISLE_FRONT) > 3 && Math.abs(cy - AISLE_MAIN) > 3) {
      pts.push({x: cx, y: AISLE_FRONT}); cy = AISLE_FRONT;
    }

    if (tx < BUNK.x + BUNK.w) {                       // heading home to the bunk
      if (Math.abs(cy - AISLE_FRONT) > 3) { pts.push({x: cx, y: AISLE_FRONT}); cy = AISLE_FRONT; }
      pts.push({x: DOOR_X, y: AISLE_FRONT});
      pts.push({x: tx, y: AISLE_FRONT});
      pts.push({x: tx, y: ty});
    } else if (ty === AISLE_MAIN) {                   // back row: step up a gap
      if (Math.abs(cy - AISLE_MAIN) > 3) {
        const gap = GAPS.reduce((a, g) => Math.abs(g - tx) < Math.abs(a - tx) ? g : a, GAPS[0]);
        pts.push({x: gap, y: AISLE_FRONT});
        pts.push({x: gap, y: AISLE_MAIN});
      }
      pts.push({x: tx, y: AISLE_MAIN});
    } else {
      if (Math.abs(cy - AISLE_FRONT) > 3) pts.push({x: cx, y: AISLE_FRONT});
      pts.push({x: tx, y: AISLE_FRONT});
    }
    return pts;
  }

  function walk(tx, ty, onArrive) {
    if (tl) { tl.cancel(); tl = null; }
    const pts = pathTo(tx, ty);
    if (!A) {
      fred.x = tx; fred.y = ty; walking = false;
      fred.working = !!onArrive; onArrive && onArrive();
      return;
    }
    walking = true;
    // anime.js composition defaults to 'replace', so a new event arriving
    // mid-walk cancels this timeline and resumes from the current position.
    tl = A.createTimeline({defaults: {ease: "linear", modifier: A.utils.round(0)}});
    let px = fred.x, py = fred.y;
    for (const p of pts) {
      const d = Math.hypot(p.x - px, p.y - py);
      if (d < 1) continue;
      const dir = Math.abs(p.x - px) > Math.abs(p.y - py)
        ? (p.x < px ? "left" : "right")
        : (p.y < py ? "up" : "down");
      tl.call(() => { fred.dir = dir; fred.flip = (dir === "left"); });
      tl.add(fred, {x: p.x, y: p.y, duration: Math.max(90, d * 22)});   // ~45 px/s
      px = p.x; py = p.y;
    }
    tl.call(() => {
      walking = false;
      fred.working = !!onArrive;
      if (onArrive) onArrive();
    });
  }

  function goSleep() {
    fred.say = "";
    walk(BED.x + 8, BED.y + 30, null);
    if (tl) tl.call(() => { fred.asleep = true; fred.working = false; });
    else { fred.asleep = true; }
  }

  // --- render --------------------------------------------------------------
  let prevX = fred.x, prevY = fred.y;

  function draw(now) {
    if (!bg) paintBackground();
    ctx.drawImage(bg, 0, 0);

    // Distance-driven stride: time-driven frames desync from movement and give
    // you the classic foot-sliding look whenever speed changes.
    const moved = Math.hypot(fred.x - prevX, fred.y - prevY);
    prevX = fred.x; prevY = fred.y;
    fred.dist += moved;
    const apart = moved > 0.05 && (Math.floor(fred.dist / 6) & 1) === 1;

    // y-sort by baseline, so Fred passes behind the back row and in front of
    // the front row with no special-casing anywhere.
    const items = racks.map(r => ({sortY: r.base, sortX: r.x, r}));
    items.push({sortY: fred.asleep ? BED.y + 14 : fred.y, sortX: fred.x, fred: true});
    items.sort((a, b) => (a.sortY - b.sortY) || (a.sortX - b.sortX));

    for (const it of items) {
      if (!it.fred) { drawRack(it.r); continue; }
      if (fred.asleep) {
        const rise = Math.floor(now / 1000) % 2 ? -1 : 0;
        blit(SLEEP, BED.x + 4, BED.y + 14 + rise, false);
        continue;
      }
      const key = (fred.dir === "left" || fred.dir === "right") ? "side" : fred.dir;
      const rows = apart ? SPR[key].slice(0, 14).concat(LEGS_APART[key]) : SPR[key];
      const bob = apart ? -1 : 0;
      const work = fred.working && !walking ? (Math.floor(now / 200) % 2 ? -1 : 0) : 0;
      ctx.fillStyle = "rgba(15,15,24,.45)";
      ctx.fillRect(Math.round(fred.x) - 4, Math.round(fred.y) - 1, 9, 2);
      blit(rows, fred.x - 6, fred.y - 18 + bob + work, fred.flip);
    }

    // Status lights: a 1px core with 3px/5px additive bloom. Cheapest
    // convincing glow in canvas, and it stays on the pixel grid.
    for (const l of leds) {
      const on = l.mode === "off" ? false
        : l.mode === "steady" ? true
        : hash2(l.x + l.phase, Math.floor((now + l.phase) / l.period)) > 0.35;
      if (!on) { ctx.fillStyle = l.mode === "off" ? C.off : C.vent; ctx.fillRect(l.x, l.y, 1, 1); continue; }
      ctx.globalCompositeOperation = "lighter";
      ctx.fillStyle = l.color;
      ctx.globalAlpha = 1;   ctx.fillRect(l.x, l.y, 1, 1);
      ctx.globalAlpha = .30; ctx.fillRect(l.x - 1, l.y - 1, 3, 3);
      ctx.globalAlpha = .10; ctx.fillRect(l.x - 2, l.y - 2, 5, 5);
      ctx.globalAlpha = 1;
      ctx.globalCompositeOperation = "source-over";
    }

    if (fred.asleep && Math.floor(now / 900) % 2) {
      ctx.fillStyle = C.dim; ctx.font = "6px ui-monospace, monospace"; ctx.textAlign = "left";
      ctx.fillText("z", BED.x + BED.w + 3, BED.y + 12);
    }

    // the bunk terminal, always on
    if (TERMINAL) {
      const t = TERMINAL, flick = (Math.floor(now / 140) % 5) === 0;
      ctx.fillStyle = flick ? "#3a5878" : "#2f4a6a";
      ctx.fillRect(t.x, t.y, t.w, t.h);
      ctx.globalCompositeOperation = "lighter"; ctx.globalAlpha = .12;
      ctx.fillStyle = "#4a7ab0"; ctx.fillRect(t.x - 3, t.y - 3, t.w + 6, t.h + 6);
      ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over";
    }

    // Vignette from nested edge bands — cheap depth, and it pushes the eye to
    // the lit aisles where the action is.
    ctx.fillStyle = "rgba(10,8,20,.16)";
    for (let i = 0; i < 5; i++) {
      ctx.fillRect(0, i * 2, W, 2); ctx.fillRect(0, H - 2 - i * 2, W, 2);
      ctx.fillRect(i * 2, 0, 2, H); ctx.fillRect(W - 2 - i * 2, 0, 2, H);
    }

    // Dialogue lives on the back wall, never over the hardware: a floating
    // bubble at this scale covers a rack whenever he stops at one.
    if (fred.say && now < fred.sayUntil) {
      ctx.font = "6px ui-monospace, monospace"; ctx.textAlign = "left";
      const tw = Math.min(ctx.measureText(fred.say).width + 8, W - 62);
      ctx.fillStyle = C.ink;      ctx.fillRect(5, 5, tw + 2, 13);
      ctx.fillStyle = C.rackFace; ctx.fillRect(6, 6, tw, 11);
      ctx.fillStyle = C.rackTop;  ctx.fillRect(6, 6, tw, 1);
      ctx.fillStyle = C.txt;      ctx.fillText(fred.say, 10, 14, tw - 8);
      if (!fred.asleep) {   // a tick above his head ties the line to him
        ctx.fillStyle = C.txt;
        ctx.fillRect(Math.round(fred.x), Math.round(fred.y) - 22, 1, 3);
      }
    }

    if (unacked > 0) {
      ctx.fillStyle = C.bad; ctx.fillRect(W - 48, 5, 45, 11);
      ctx.fillStyle = C.ink; ctx.font = "6px ui-monospace, monospace"; ctx.textAlign = "left";
      ctx.fillText(unacked + " to ack", W - 45, 13);
    }
  }

  // 30fps is ample for a slow-walking character and halves the cost of a widget
  // that is on all day.
  const MIN_DT = 1000 / 30;
  let running = true, lastT = 0;
  function frame(now) {
    requestAnimationFrame(frame);
    if (!running || now - lastT < MIN_DT) return;
    lastT = now;
    if (A) A.engine.update();          // advance tweens exactly once, then draw
    if (!fred.asleep && !walking && Date.now() - fred.lastEvent > 90000) goSleep();
    draw(now);
  }
  if (A) A.engine.useDefaultMainLoop = false;

  // rAF only stops for hidden *tabs*; a dashboard is often visible-but-scrolled
  if (window.IntersectionObserver) {
    new IntersectionObserver(e => { running = e[0].isIntersecting; }, {threshold: 0}).observe(cvs);
  }

  // Integer upscale only — a fractional scale makes art pixels unequal sizes
  // and the grid visibly shimmers.
  function fit() {
    const avail = (cvs.parentElement ? cvs.parentElement.clientWidth : W) - 8;
    const s = Math.max(1, Math.min(5, Math.floor(avail / W)));
    cvs.style.width = (W * s) + "px";
    cvs.style.height = (H * s) + "px";
  }
  addEventListener("resize", fit);

  // --- bind to the event stream -------------------------------------------
  function rackFor(subject) {
    if (!subject) return null;
    for (const r of racks) {
      if (r.node === subject || r.node.endsWith(subject)) return r;
      if (r.guests.some(g => g.name === subject)) return r;
    }
    return null;
  }

  function react(e) {
    if (!e || e.kind === "ack") return;
    fred.lastEvent = Date.now();
    fred.asleep = false;
    const msg = (e.message || "").slice(0, 24);
    fred.say = e.severity === "escalate" ? "! " + (e.subject || "problem")
      : e.kind === "resolved" ? "all clear: " + (e.subject || "")
      : e.kind === "update" ? "updating " + (e.subject || "")
      : e.kind === "backup" ? "checking backups"
      : e.kind === "finding" ? (e.subject ? e.subject + ": " : "") + msg
      : (msg || "walking the floor");
    fred.sayUntil = performance.now() + 12000;

    const r = rackFor(e.subject);
    if (r) walk(r.x + RACK_W / 2, r.base === ROW_A_Y ? AISLE_MAIN : AISLE_FRONT, () => {});
    else walk(GAPS[2], AISLE_FRONT, null);
  }

  async function load() {
    try {
      const f = await (await fetch("/api/fleet")).json();
      const raw = (f.nodes && f.nodes.length)
        ? f.nodes : [...new Set((f.guests || []).map(g => g.node))];
      layout(raw.map(n => (typeof n === "string" ? n : (n.node || n.name || "?"))));
      (f.guests || []).forEach(g => {
        const r = racks.find(r => r.node === g.node);
        if (r) r.guests.push({name: g.name, status: g.status});
      });
    } catch { layout(["node"]); }
    rebuildLeds();

    try {
      const d = await (await fetch("/api/events?limit=60")).json();
      const evs = d.events || [];
      const acked = new Set(evs.filter(e => e.kind === "ack").map(e => e.ref));
      unacked = evs.filter(e => e.severity === "escalate" && !acked.has(e.id)).length;
      const last = [...evs].reverse().find(e => e.kind !== "ack");
      // An hours-old event should find him asleep, not mid-stride.
      if (last && Date.now() - new Date(last.ts).getTime() < 90000) react(last);
    } catch {}
    fit();
  }

  const es = new EventSource("/api/stream");
  es.onmessage = m => {
    try {
      const e = JSON.parse(m.data);
      if (e.kind === "ack") { unacked = Math.max(0, unacked - 1); return; }
      react(e);
    } catch (err) {
      // Swallowing this kept the stream alive but left Fred frozen with no
      // signal anywhere — the symptom looked like "he just stopped walking".
      console.error("room: failed to handle event", err);
    }
  };

  load().then(() => requestAnimationFrame(frame));
  setInterval(load, 60000);
})();
