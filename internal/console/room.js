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

  // 320x200. Chosen as a modern square-pixel 8:5 canvas, not as VGA nostalgia:
  // this widget sizes its own CSS box, so 16:9 divisibility matters far less
  // than an aspect that suits a room interior. 320x180 would have cost 20 rows
  // of height that a top-down room actually needs.
  const W = 320, H = 200, TILE = 16;
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
    // The only other saturated colours in the scene, and they get to be loud:
    // a rack on fire should be the first thing your eye lands on.
    fCore:"#fff6c2", fHot:"#ffe066", fMid:"#feae34", fEdge:"#e43b44",
    fDark:"#a02c2c", smoke:"#4a5878",
    water:"#4aa8e8", waterLit:"#9fdcff", bucket:"#7a6a58", bucketRim:"#a89680",
    steam:"#c0cbdc",
    neonHot:"#ff5cc8", neonCore:"#ffd6f2", neonDim:"#8a3a6e", neonDead:"#33263a",
    tube:"#241c2c", cable:"#3d3550", cableLit:"#544a68",
    ledOn:"#e43b44", ledDim:"#4a1d22", bezel:"#1a1622", bezelTop:"#2c2636",
    counter:"#4a4256", counterTop:"#6b6178", hob:"#2a2432", pot:"#8b9bb4",
    chairA:"#463a52", chairB:"#574868", book:"#a8524a", bookB:"#4a6ea8",
    skyDay:"#6f9fd0", skyDusk:"#8a6a8c", skyNight:"#141d33",
    sun:"#ffe9a8", moon:"#dfe6f2", star:"#c0cbdc", cloud:"#8ea6c4",
    rain:"#7fb0e0", snow:"#e8f0ff", frame:"#3a3040",
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
  // Seated at the terminal, seen from behind, hands out on the desk. Two frames
  // that differ only in the arms -- at 12px wide that reads as typing.
  const SIT = [
    ["....hhhh....", "..hhhhhhhh..", "..hhhhhhhh..", "..hhhhhhhh..",
     "...hhhhhh...", "..kCCCCCCk..", ".kCCCCCCCCk.", ".sCCCCCCCCs.",
     ".sCCCCCCCCs.", "..CCCCCCCC..", "..PPPPPPPP..", "..PPP..PPP.."],
    ["....hhhh....", "..hhhhhhhh..", "..hhhhhhhh..", "..hhhhhhhh..",
     "...hhhhhh...", "..kCCCCCCk..", ".kCCCCCCCCk.", "s.CCCCCCCC.s",
     ".sCCCCCCCCs.", "..CCCCCCCC..", "..PPPPPPPP..", "..PPP..PPP.."],
  ];
  // Crouched at a rack with an arm raised to a slot. Read at a glance as
  // "he is doing something to that machine" rather than merely standing by it.
  const INSPECT = [
    ["............", "....hhhh....", "..hhhhhhhh..", "..hhhhhhhh..",
     "..hhhhhhhh..", "...hhhhhh...", "..kCCCCCCk..", ".sCCCCCCCCk.",
     ".sCCCCCCCCs.", "..CCCCCCCC..", "..PPPPPPPP..", "..PPP..PPP..",
     "..bb....bb..", "..bb....bb.."],
    ["....s.......", "....hhhh....", "..hhhhhhhh..", "..hhhhhhhh..",
     "..hhhhhhhh..", "...hhhhhh...", "..kCCCCCCk..", "..CCCCCCCCk.",
     ".sCCCCCCCCs.", "..CCCCCCCC..", "..PPPPPPPP..", "..PPP..PPP..",
     "..bb....bb..", "..bb....bb.."],
  ];
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
  const WALL_H = 46, FLOOR_B = 192;           // taller wall, to host a real window
  const BUNK = {x:6, y:50, w:102, h:138};     // the studio
  const BED  = {x:14, y:58, w:38, h:46};      // properly sized now
  const WINDOW  = {x:24, y:6, w:64, h:34};    // ~20% of canvas width
  const KITCHEN = {x:66, y:58, w:38, h:30};   // its own corner, away from the bed
  const CHAIR   = {x:70, y:128, w:22, h:22};
  const SHELF   = {x:12, y:112, w:9, h:28};
  const DOOR = {y0:158, y1:186};
  const RACK_W = 18, RACK_H = 32, TOP_H = 6;
  const ROW_A_Y = 104, ROW_B_Y = 168;
  const RACK_X = [130, 176, 222, 268];
  const AISLE_MAIN = 128, AISLE_FRONT = 184;
  const GAPS = [122, 162, 208, 254, 298];
  const DOOR_X = BUNK.x + BUNK.w + 12;
  const SIGN  = {x:132, y:9,  w:58, h:14};    // neon, mounted on the back wall
  const CLOCK = {x:246, y:12, w:40, h:20};

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
    const D = {x: BUNK.x + 8, y: BUNK.y + 78, w: 44, h: 20};
    b.fillStyle = C.rackEdge; b.fillRect(D.x - 1, D.y - 1, D.w + 2, D.h + 2);
    b.fillStyle = C.rackFace; b.fillRect(D.x, D.y, D.w, D.h);
    b.fillStyle = C.rackTop;  b.fillRect(D.x, D.y, D.w, 4);
    b.fillStyle = C.rackEdge; b.fillRect(D.x + 8, D.y - 10, 18, 11);  // monitor
    b.fillStyle = C.vent;     b.fillRect(D.x + 9, D.y - 9, 16, 9);
    b.fillStyle = C.rackEdge; b.fillRect(D.x + 32, D.y + 7, 5, 5);    // mug
    b.fillStyle = C.bedFrame; b.fillRect(D.x + 33, D.y + 8, 3, 3);
    b.fillStyle = C.rackEdge; b.fillRect(D.x + 14, D.y + 22, 13, 10); // chair
    b.fillStyle = C.rug;      b.fillRect(D.x + 15, D.y + 23, 11, 8);

    // kitchenette, now its own corner rather than crowding the bed
    b.fillStyle = C.rackEdge; b.fillRect(KITCHEN.x - 1, KITCHEN.y - 1, KITCHEN.w + 2, KITCHEN.h + 2);
    b.fillStyle = C.counter;    b.fillRect(KITCHEN.x, KITCHEN.y, KITCHEN.w, KITCHEN.h);
    b.fillStyle = C.counterTop; b.fillRect(KITCHEN.x, KITCHEN.y, KITCHEN.w, 4);
    b.fillStyle = C.hob;
    b.fillRect(KITCHEN.x + 4, KITCHEN.y + 9, 7, 7);
    b.fillRect(KITCHEN.x + 15, KITCHEN.y + 9, 7, 7);
    b.fillStyle = C.rackEdge;   b.fillRect(KITCHEN.x + 3, KITCHEN.y + 21, KITCHEN.w - 6, 1);
    b.fillStyle = C.counterTop; b.fillRect(KITCHEN.x + 26, KITCHEN.y + 8, 9, 12);  // sink
    b.fillStyle = C.hob;        b.fillRect(KITCHEN.x + 28, KITCHEN.y + 10, 5, 8);

    // armchair, facing the room
    b.fillStyle = C.rackEdge; b.fillRect(CHAIR.x - 1, CHAIR.y - 1, CHAIR.w + 2, CHAIR.h + 2);
    b.fillStyle = C.chairA;   b.fillRect(CHAIR.x, CHAIR.y, CHAIR.w, CHAIR.h);
    b.fillStyle = C.chairB;   b.fillRect(CHAIR.x + 2, CHAIR.y + 4, CHAIR.w - 4, CHAIR.h - 6);
    b.fillStyle = C.chairA;
    b.fillRect(CHAIR.x, CHAIR.y + 4, 3, CHAIR.h - 5);                 // arms
    b.fillRect(CHAIR.x + CHAIR.w - 3, CHAIR.y + 4, 3, CHAIR.h - 5);
    b.fillStyle = C.quilt;                                            // cushion
    b.fillRect(CHAIR.x + 4, CHAIR.y + CHAIR.h - 8, CHAIR.w - 8, 4);
    b.fillStyle = C.rackEdge;
    b.fillRect(CHAIR.x + 3, CHAIR.y + 3, CHAIR.w - 6, 1);             // back seam

    // a few books
    b.fillStyle = C.rackEdge; b.fillRect(SHELF.x - 1, SHELF.y - 1, SHELF.w + 2, SHELF.h + 2);
    b.fillStyle = C.bedFrame; b.fillRect(SHELF.x, SHELF.y, SHELF.w, SHELF.h);
    for (let i = 0; i < 5; i++) {
      b.fillStyle = i % 2 ? C.book : C.bookB;
      b.fillRect(SHELF.x + 1 + (i % 3), SHELF.y + 2 + i * 3, 3, 2);
    }

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
    pool(176, AISLE_MAIN - 4, 72, 19);
    pool(262, AISLE_MAIN - 4, 58, 17);
    pool(206, AISLE_FRONT - 2, 84, 18);
    pool(BUNK.x + 36, BUNK.y + 92, 38, 16);
    b.globalAlpha = 1; b.globalCompositeOperation = "source-over";
    TERMINAL = {x: D.x + 9, y: D.y - 9, w: 16, h: 9};
  }

  // Two octaves of the stable hash: a slow one that makes the flame writhe and a
  // fast one that makes it crackle. One octave alone either strobes or crawls.
  function flameHeight(seed, i, w, now) {
    const slow = hash2(seed + i * 7, Math.floor(now / 110));
    const fast = hash2(seed + i * 131, Math.floor(now / 55));
    const taper = 1 - Math.abs((i / Math.max(1, w - 1)) * 2 - 1);   // tallest mid-rack
    return Math.round((2 + (slow * 0.65 + fast * 0.35) * 11) * (0.3 + taper));
  }

  // --- the bucket brigade ---------------------------------------------------
  // One throw cycle, derived entirely from the clock so it needs no state:
  // wind up, throw, water arcs over, lands, steam. The fire is knocked down
  // where the water hits and comes straight back, because the rack is burning
  // until somebody acks the escalation -- not until Fred works harder.
  const THROW_MS = 1700, RELEASE = 0.42, FLIGHT = 0.34;

  function throwPhase(now, seed) {
    return ((now + seed * 211) % THROW_MS) / THROW_MS;
  }

  // 0 while the water is in the air, rising to 1 over the seconds after it
  // lands, so the flames recover rather than snapping back.
  function doused(now, seed) {
    const p = throwPhase(now, seed);
    const land = RELEASE + FLIGHT;
    if (p < land) return 1;
    return Math.min(1, 0.25 + (p - land) / (1 - land) * 0.9);
  }

  // He is only fighting the rack he actually walked to.
  function fightingHere(r) {
    return fred.activity === "fight" && !walking && !fred.asleep &&
      Math.abs(fred.x - (r.x + RACK_W / 2)) < 12 &&
      Math.abs(fred.y - (r.base === ROW_A_Y ? AISLE_MAIN : AISLE_FRONT)) < 8;
  }

  function drawBucket(fx, fy, now, seed) {
    const p = throwPhase(now, seed);
    // low at the side, swung up and over for the throw, then back down
    let bx = fx + 5, by = fy - 9, full = true;
    if (p > RELEASE - 0.12 && p < RELEASE) { bx = fx + 4; by = fy - 15; }
    else if (p >= RELEASE && p < RELEASE + 0.18) { bx = fx + 2; by = fy - 17; full = false; }
    else if (p >= RELEASE + 0.18) { bx = fx + 5; by = fy - 9; full = false; }
    bx = Math.round(bx); by = Math.round(by);
    ctx.fillStyle = C.bucketRim; ctx.fillRect(bx, by, 4, 1);
    ctx.fillStyle = C.bucket;
    ctx.fillRect(bx, by + 1, 1, 3); ctx.fillRect(bx + 3, by + 1, 1, 3);
    ctx.fillRect(bx + 1, by + 4, 2, 1);
    if (full) { ctx.fillStyle = C.water; ctx.fillRect(bx + 1, by + 1, 2, 3); }
  }

  function drawWater(fx, fy, tx, ty, now, seed) {
    const p = throwPhase(now, seed);
    if (p < RELEASE || p > RELEASE + FLIGHT) return;
    const t = (p - RELEASE) / FLIGHT;
    const sx = fx + 2, sy = fy - 16;
    for (let i = 0; i < 7; i++) {
      const tt = Math.max(0, t - i * 0.05);
      if (tt <= 0) continue;
      const x = sx + (tx - sx) * tt + (i - 3) * 0.6;
      // a lobbed arc: up early, down onto the rack
      const y = sy + (ty - sy) * tt - Math.sin(tt * Math.PI) * 13;
      ctx.fillStyle = i % 3 === 0 ? C.waterLit : C.water;
      ctx.fillRect(Math.round(x), Math.round(y), 1, 1);
    }
    // splash on arrival
    if (t > 0.86) {
      ctx.fillStyle = C.waterLit;
      for (let i = 0; i < 5; i++) {
        const s = (t - 0.86) / 0.14;
        ctx.fillRect(Math.round(tx - 4 + i * 2), Math.round(ty - s * 3 - (i % 2)), 1, 1);
      }
    }
  }

  function drawSteam(tx, ty, now, seed) {
    const p = throwPhase(now, seed);
    const land = RELEASE + FLIGHT;
    if (p < land || p > land + 0.42) return;
    const t = (p - land) / 0.42;
    ctx.fillStyle = C.steam;
    ctx.globalAlpha = 0.55 * (1 - t);
    for (let i = 0; i < 6; i++) {
      const x = tx - 5 + ((hash2(seed + i * 17, i) * 11) | 0);
      ctx.fillRect(Math.round(x), Math.round(ty - 2 - t * 16 - i), 1, 1);
    }
    ctx.globalAlpha = 1;
  }

  function drawFire(x, y, w, now, seed, wet) {
    wet = wet === undefined ? 1 : wet;
    for (let i = 0; i < w; i++) {
      const h = Math.max(1, Math.round(flameHeight(seed, i, w, now) * wet));
      for (let j = 0; j < h; j++) {
        const f = j / Math.max(1, h);            // 0 at the base, 1 at the tip
        ctx.fillStyle = f > 0.82 ? C.fDark : f > 0.58 ? C.fEdge
                      : f > 0.3 ? C.fMid : f > 0.12 ? C.fHot : C.fCore;
        ctx.fillRect(x + i, y - j, 1, 1);
      }
    }
    // embers and smoke climbing out of the top, wrapping on a long cycle
    for (let k = 0; k < 5; k++) {
      const ph = (now / (260 + k * 70) + hash2(seed, k) * 6) % 1;
      const ex = x + Math.round(hash2(seed + k * 31, Math.floor(now / 900 + k)) * (w - 1));
      const ey = y - 12 - Math.round(ph * 20);
      if (ey < 2) continue;
      ctx.fillStyle = ph < 0.45 ? C.fMid : C.smoke;
      ctx.fillRect(ex + (ph > 0.6 ? 1 : 0), ey, 1, 1);
    }
  }

  function fireGlow(x, y, w, now, seed, wet) {
    wet = wet === undefined ? 1 : wet;
    // Elliptical and drawn per scanline: nested rectangles read as a lit box
    // hanging over the rack, which is exactly how this looked on the first pass.
    // It also flickers -- a steady glow reads as a lamp, not a fire.
    const puls = (0.72 + hash2(seed, Math.floor(now / 70)) * 0.55) * wet;
    const cx = x + w / 2, rx = w + 12, ry = 17;
    ctx.globalCompositeOperation = "lighter";
    ctx.fillStyle = "#ff8a3c";
    for (let dy = -ry; dy <= ry; dy++) {
      const k = 1 - (dy * dy) / (ry * ry);
      if (k <= 0) continue;
      const hw = Math.round(rx * Math.sqrt(k));
      ctx.globalAlpha = 0.085 * k * k * puls;
      ctx.fillRect(Math.round(cx - hw), Math.round(y + dy), hw * 2, 1);
    }
    ctx.globalAlpha = 1;
    ctx.globalCompositeOperation = "source-over";
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
    ctx.fillStyle = burning.has(r.node) ? C.fMid : C.dim;
    ctx.font = "5px ui-monospace, monospace"; ctx.textAlign = "center";
    ctx.fillText(r.node.replace("prxy-", ""), x + RACK_W / 2, top - 2);
    ctx.textAlign = "left";
    if (burning.has(r.node)) {
      const seed = r.x * 31 + r.base;
      // Knocked down while the water lands, only to climb back: he is not
      // losing because he is bad at this, he is losing because the escalation
      // is still unacked.
      const wet = fightingHere(r) ? doused(nowMs, seed) : 1;
      drawFire(x, top, RACK_W, nowMs, seed, wet);
      pendingGlow.push([x, top - 6, RACK_W, seed, wet]);
      if (fightingHere(r)) pendingSteam.push([x + RACK_W / 2, top, seed]);
    }
  }



  // --- wall fittings ----------------------------------------------------------
  // A 3x5 stroke font. Anything smaller stops being letters, anything larger
  // does not fit the wall above the racks.
  const GLYPH = {
    T:["###",".#.",".#.",".#.",".#."], H:["#.#","#.#","###","#.#","#.#"],
    E:["###","#..","##.","#..","###"], L:["#..","#..","#..","#..","###"],
    I:["###",".#.",".#.",".#.","###"], G:[".##","#..","#.#","#.#",".##"],
    A:[".#.","#.#","###","#.#","#.#"], B:["##.","#.#","##.","#.#","##."],
    " ":["...","...","...","...","..."],
  };
  const DIGIT = {
    "0":["###","#.#","#.#","#.#","###"], "1":["..#","..#","..#","..#","..#"],
    "2":["###","..#","###","#..","###"], "3":["###","..#","###","..#","###"],
    "4":["#.#","#.#","###","..#","..#"], "5":["###","#..","###","..#","###"],
    "6":["###","#..","###","#.#","###"], "7":["###","..#","..#","..#","..#"],
    "8":["###","#.#","###","#.#","###"], "9":["###","#.#","###","..#","###"],
  };
  const SIGN_TEXT = "THE LIGHT LAB";
  // Which letters have given up. Index into SIGN_TEXT: the second L is dead
  // outright, the I and the final B stutter.
  const DEAD = new Set([10]);
  const FLICKER = new Set([5, 12]);

  function glyphAt(map, ch, x, y, colour) {
    const g = map[ch];
    if (!g) return;
    ctx.fillStyle = colour;
    for (let r = 0; r < 5; r++)
      for (let c = 0; c < 3; c++)
        if (g[r][c] === "#") ctx.fillRect(x + c, y + r, 1, 1);
  }

  // Deterministic stutter: a dying tube has a rhythm, not a coin flip per frame.
  function tubeLit(i, now) {
    if (DEAD.has(i)) return false;
    if (!FLICKER.has(i)) return true;
    const t = Math.floor(now / 90) + i * 13;
    return hash2(i * 97, t) > 0.28;
  }

  function drawSign(now) {
    let x = SIGN.x + 2;
    const y = SIGN.y + 5;
    // glow first, so the tubes sit inside their own halo
    ctx.globalCompositeOperation = "lighter";
    for (let i = 0; i < SIGN_TEXT.length; i++) {
      const ch = SIGN_TEXT[i];
      if (ch !== " " && tubeLit(i, now)) {
        ctx.globalAlpha = 0.10; ctx.fillStyle = C.neonHot;
        ctx.fillRect(x - 2, y - 2, 7, 9);
      }
      x += ch === " " ? 2 : 4;
    }
    ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over";

    x = SIGN.x + 2;
    for (let i = 0; i < SIGN_TEXT.length; i++) {
      const ch = SIGN_TEXT[i];
      if (ch !== " ") {
        const lit = tubeLit(i, now);
        glyphAt(GLYPH, ch, x, y, lit ? C.neonCore : C.neonDead);
        if (lit) {   // hot core, one pixel in, is what makes it read as neon
          ctx.fillStyle = C.neonHot;
          ctx.fillRect(x + 1, y + 2, 1, 1);
        }
      }
      x += ch === " " ? 2 : 4;
    }
  }

  function drawClock(now) {
    const d = new Date();
    const hh = String(d.getHours()).padStart(2, "0");
    const mm = String(d.getMinutes()).padStart(2, "0");
    const x = CLOCK.x + 5, y = CLOCK.y + 7;
    // dim ghost segments, so the unlit parts of the display still read as a display
    for (let i = 0; i < 4; i++) glyphAt(DIGIT, "8", x + i * 4 + (i > 1 ? 3 : 0), y, C.ledDim);
    glyphAt(DIGIT, hh[0], x, y, C.ledOn);
    glyphAt(DIGIT, hh[1], x + 4, y, C.ledOn);
    glyphAt(DIGIT, mm[0], x + 11, y, C.ledOn);
    glyphAt(DIGIT, mm[1], x + 15, y, C.ledOn);
    // colon on the seconds, which is the only thing that says it is running
    ctx.fillStyle = d.getSeconds() % 2 ? C.ledDim : C.ledOn;
    ctx.fillRect(x + 9, y + 1, 1, 1);
    ctx.fillRect(x + 9, y + 3, 1, 1);
    ctx.globalCompositeOperation = "lighter";
    ctx.globalAlpha = 0.08; ctx.fillStyle = C.ledOn;
    ctx.fillRect(CLOCK.x + 2, CLOCK.y + 4, CLOCK.w - 4, 11);
    ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over";
  }

  // --- the view outside ------------------------------------------------------
  // Season is carried almost entirely by PALETTE, not by shapes: the sky, hills
  // and ground are ~95% of this aperture and the tree canopy under 6%. So four
  // palettes share one canopy mask, and only winter swaps in a bare silhouette.
  // Colours are drawn from a single parent (Resurrect 64) so seasons harmonise.
  let weather = null;   // {code, temp_c, is_day, wind_kph, stale} or null

  const SEASONS = {
    spring: {skyHi:"#8fd3ff", skyLo:"#c7dcd0", hill:"#92a984",
             cDk:"#676633", cMid:"#a2a947", cLt:"#d5e04b", accent:"#eaaded",
             gDk:"#547e64", gMid:"#91db69", gLt:"#cddf6c", trunk:"#4c3e24"},
    summer: {skyHi:"#4d9be6", skyLo:"#8fd3ff", hill:"#547e64",
             cDk:"#165a4c", cMid:"#239063", cLt:"#1ebc73", accent:null,
             gDk:"#239063", gMid:"#1ebc73", gLt:"#91db69", trunk:"#4c3e24"},
    autumn: {skyHi:"#7f708a", skyLo:"#9babb2", hill:"#966c6c",
             cDk:"#9e4539", cMid:"#cd683d", cLt:"#e6904e", accent:"#fbb954",
             gDk:"#676633", gMid:"#a2a947", gLt:"#ab947a", trunk:"#4c3e24"},
    winter: {skyHi:"#7f708a", skyLo:"#9babb2", hill:"#9babb2",
             cDk:"#3e3546", cMid:"#625565", cLt:"#9babb2", accent:null,
             gDk:"#9babb2", gMid:"#c7dcd0", gLt:"#ffffff", trunk:"#3e3546"},
  };

  function seasonNow(d) {
    const m = d.getMonth();                       // northern hemisphere
    if (m <= 1 || m === 11) return "winter";
    if (m <= 4) return "spring";
    if (m <= 7) return "summer";
    return "autumn";
  }

  // WMO codes, collapsed to what can be drawn at this size.
  function weatherKind() {
    if (!weather) return "clear";
    const c = weather.code;
    if ((c >= 71 && c <= 77) || c === 85 || c === 86) return "snow";
    if (c === 95 || c === 96 || c === 99) return "storm";
    if ((c >= 51 && c <= 67) || (c >= 80 && c <= 82)) return "rain";
    if (c >= 45 && c <= 48) return "fog";
    if (c === 2 || c === 3) return "cloud";
    if (c === 1) return "part";
    return "clear";
  }

  function isNight() {
    const h = new Date().getHours();
    return h < 6 || h >= 20;
  }

  // Blend a colour toward the sky. Atmospheric perspective and fog are the same
  // operation at different strengths, so they share one function.
  function haze(hex, skyHex, t) {
    const p = h => [parseInt(h.slice(1,3),16), parseInt(h.slice(3,5),16), parseInt(h.slice(5,7),16)];
    const [r,g,b] = p(hex), [sr,sg,sb] = p(skyHex);
    const m = (a,c) => Math.round(a + (c - a) * t).toString(16).padStart(2,"0");
    return "#" + m(r,sr) + m(g,sg) + m(b,sb);
  }

  // Canopy mask, shared by every leafed season. 1 = leaf, 2 = lit, 3 = shadow.
  const CANOPY = [
    "..11111..", ".1122111.", "112222111", "132222211",
    "133222211", ".13222111", "..133111.", "...1.1...",
  ];
  const BRANCHES = [
    "....#....", "..#.#.#..", ".#..#..#.", "..#.#.#..",
    "....#....", "....#....", "....#....", "....#....",
  ];

  let landscape = null, landscapeKey = "";

  // Baked once per (season, night, weather-kind) rather than per frame: this is
  // the static half of the view and redrawing it every tick would be the only
  // expensive thing in the scene.
  function bakeLandscape(key, season, night, kind) {
    const P = SEASONS[season];
    const cv = document.createElement("canvas");
    cv.width = WINDOW.w; cv.height = WINDOW.h;
    const g = cv.getContext("2d");
    const wet = kind === "rain" || kind === "storm";
    const fog = kind === "fog";

    // Sky: banded, never a smooth gradient -- banding is the idiom and
    // dithering at this size reads as dirt.
    let hi = P.skyHi, lo = P.skyLo;
    if (night) { hi = "#141d33"; lo = "#243053"; }
    else if (wet) { hi = haze(P.skyHi, "#4a4a58", 0.45); lo = haze(P.skyLo, "#6a6a76", 0.4); }
    else if (fog) { hi = "#9babb2"; lo = "#c7dcd0"; }
    const horizon = Math.round(WINDOW.h * 0.56);
    g.fillStyle = hi; g.fillRect(0, 0, WINDOW.w, Math.round(horizon * 0.55));
    g.fillStyle = lo; g.fillRect(0, Math.round(horizon * 0.55), WINDOW.w, horizon - Math.round(horizon * 0.55));

    if (night && !fog && kind !== "cloud" && !wet) {
      for (let i = 0; i < 22; i++) {
        g.fillStyle = "#c0cbdc";
        g.fillRect(2 + ((hash2(i * 41, 7) * (WINDOW.w - 4)) | 0),
                   2 + ((hash2(i * 17, 3) * (horizon - 6)) | 0), 1, 1);
      }
    }

    // Far hills: one flat band, hazed toward the sky. Fog just cranks the blend.
    const hillT = fog ? 0.85 : wet ? 0.5 : 0.35;
    g.fillStyle = haze(P.hill, lo, hillT);
    for (let x = 0; x < WINDOW.w; x++) {
      const hgt = 4 + Math.round(Math.sin(x / 9) * 2 + Math.sin(x / 23) * 2);
      g.fillRect(x, horizon - hgt, 1, hgt);
    }

    // Ground: the loudest season signal, since it is ~44% of the aperture.
    const gT = fog ? 0.55 : wet ? 0.25 : 0;
    g.fillStyle = haze(P.gMid, lo, gT); g.fillRect(0, horizon, WINDOW.w, WINDOW.h - horizon);
    g.fillStyle = haze(P.gDk, lo, gT);  g.fillRect(0, horizon, WINDOW.w, 1);
    g.fillStyle = haze(P.gLt, lo, gT);
    for (let i = 0; i < 26; i++) {
      const x = (hash2(i * 13, 5) * WINDOW.w) | 0;
      const y = horizon + 2 + ((hash2(i * 29, 9) * (WINDOW.h - horizon - 3)) | 0);
      g.fillRect(x, y, 1, 1);
    }

    // The tree. Trunk crosses the horizon line -- one overlapping edge does more
    // for depth than any amount of haze.
    const tx = Math.round(WINDOW.w * 0.66), ty = horizon - 12;
    g.fillStyle = haze(P.trunk, lo, gT);
    g.fillRect(tx + 4, ty + 6, 2, 10);
    const art = season === "winter" ? BRANCHES : CANOPY;
    for (let r = 0; r < art.length; r++) {
      for (let c = 0; c < art[r].length; c++) {
        const ch = art[r][c];
        if (ch === ".") continue;
        g.fillStyle = season === "winter" ? P.cMid
          : ch === "2" ? P.cLt : ch === "3" ? P.cDk : P.cMid;
        g.fillRect(tx + c, ty + r, 1, 1);
      }
    }
    if (season === "winter") {                       // snow sits ABOVE branches,
      g.fillStyle = "#ffffff";                       // never thickening them
      for (const [c, r] of [[4,0],[2,1],[6,1],[1,2],[7,2]]) g.fillRect(tx + c, ty + r - 1, 1, 1);
    } else if (P.accent) {
      for (let i = 0; i < 5; i++) {
        g.fillStyle = P.accent;
        g.fillRect(tx + 1 + ((hash2(i * 7, 2) * 7) | 0), ty + 1 + ((hash2(i * 11, 4) * 6) | 0), 1, 1);
      }
    }

    landscape = cv; landscapeKey = key;
  }

  // --- weather particles ------------------------------------------------------
  // Persistent objects, never re-randomised per frame: random placement each
  // tick is the definition of television static, not rain.
  const drops = [];
  function ensureDrops(kind, windKph) {
    const want = kind === "storm" ? 34 : kind === "rain" ? 20 : kind === "snow" ? 22 : 0;
    while (drops.length > want) drops.pop();
    while (drops.length < want) {
      drops.push({x: Math.random() * WINDOW.w, y: Math.random() * WINDOW.h,
                  layer: drops.length % 3, phase: Math.random() * 6.283});
    }
  }

  const splashes = [];

  function drawWeather(now, dt) {
    const kind = weatherKind();
    if (kind === "clear" || kind === "part" || kind === "cloud" || kind === "fog") {
      drops.length = 0;
      return;
    }
    const windMs = ((weather && weather.wind_kph) || 0) / 3.6;
    ensureDrops(kind, windMs);
    const snow = kind === "snow";
    // Snow is ~9x lighter than rain, so the same wind skews it far further --
    // that asymmetry is the strongest rain/snow cue after elongation.
    const ang = Math.min(Math.atan2(windMs, snow ? 1.0 : 9.0), 0.7);
    const sp = [0.55, 0.8, 1.05];

    for (const d of drops) {
      const speed = (snow ? 0.35 : 2.4) * sp[d.layer];
      d.x += Math.sin(ang) * speed + (snow ? Math.sin(now / 900 + d.phase) * 0.20 : 0);
      d.y += Math.cos(ang) * speed;
      if (d.y > WINDOW.h) {
        if (!snow) splashes.push({x: d.x, y: WINDOW.h - 1, t: 0});
        d.y = -2; d.x = Math.random() * WINDOW.w;
      }
      if (d.x < -4) d.x += WINDOW.w + 8;
      if (d.x > WINDOW.w + 4) d.x -= WINDOW.w + 8;

      const px = (WINDOW.x + d.x) | 0, py = (WINDOW.y + d.y) | 0;
      if (snow) {
        ctx.fillStyle = d.layer === 2 ? "#ffffff" : "#c7dcd0";
        ctx.fillRect(px, py, d.layer === 2 ? 2 : 1, d.layer === 2 ? 2 : 1);
      } else {
        // length rises with speed: streak = velocity x exposure, so they stay
        // coupled and the rain reads as physical rather than as wallpaper
        const len = Math.max(3, Math.round(speed * 1.9));
        ctx.fillStyle = d.layer === 2 ? "#9fdcff" : d.layer === 1 ? "#7fb0e0" : "#5f8fb8";
        for (let i = 0; i < len; i++) {
          const sx = px - ((i * Math.tan(ang)) | 0), sy = py - i;
          if (sy < WINDOW.y || sy >= WINDOW.y + WINDOW.h) continue;
          ctx.fillRect(sx, sy, 1, 1);
        }
      }
    }

    // Impact splashes: the detail that convinces the eye there is a ground
    // plane, which retroactively sells the streaks as falling toward it.
    for (let i = splashes.length - 1; i >= 0; i--) {
      const s2 = splashes[i];
      s2.t += dt;
      if (s2.t > 130) { splashes.splice(i, 1); continue; }
      ctx.fillStyle = "#9fdcff";
      const bx = (WINDOW.x + s2.x) | 0, by = WINDOW.y + s2.y;
      if (s2.t < 65) ctx.fillRect(bx, by, 1, 1);
      else { ctx.fillRect(bx - 1, by, 1, 1); ctx.fillRect(bx + 1, by, 1, 1); }
    }
  }

  // --- lightning --------------------------------------------------------------
  // Two excursions per event, ~180ms total, well under WCAG's three-flashes-per
  // -second ceiling. The window is lit fully; the room only takes the spill,
  // and the full-amplitude area stays far below the 87,296 CSS px threshold at
  // any sane scale. Peak is a lift, never white -- this is a dashboard someone
  // leaves open all day.
  const FLASH = [[0, 1.0, 0.40], [17, 1.0, 0.40], [33, 0.45, 0.18],
                 [133, 0.60, 0.24], [150, 0.25, 0.10]];
  let nextStrike = 0, strikeAt = -1;
  const reduceMotion = typeof matchMedia === "function" &&
                       matchMedia("(prefers-reduced-motion: reduce)").matches;

  function lightning(now) {
    if (weatherKind() !== "storm" || reduceMotion) return 0;
    if (!nextStrike) nextStrike = now + 4000 + Math.random() * 12000;
    if (now >= nextStrike) { strikeAt = now; nextStrike = now + 8000 + Math.random() * 17000; }
    if (strikeAt < 0) return 0;
    const t = now - strikeAt;
    if (t > 200) { strikeAt = -1; return 0; }
    let win = 0, room = 0;
    for (const [at, w2, r2] of FLASH) if (t >= at && t < at + 17) { win = w2; room = r2; }
    if (win > 0) {
      ctx.globalCompositeOperation = "lighter";
      ctx.globalAlpha = win * 0.55; ctx.fillStyle = "#fdcbb0";
      ctx.fillRect(WINDOW.x, WINDOW.y, WINDOW.w, WINDOW.h);
      ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over";
    }
    return room;
  }

  function drawWindow(now, dt) {
    const season = seasonNow(new Date()), night = isNight(), kind = weatherKind();
    const key = season + "|" + night + "|" + kind;
    if (key !== landscapeKey) bakeLandscape(key, season, night, kind);

    ctx.fillStyle = C.frame;
    ctx.fillRect(WINDOW.x - 3, WINDOW.y - 3, WINDOW.w + 6, WINDOW.h + 6);
    ctx.drawImage(landscape, WINDOW.x, WINDOW.y);

    // Clouds drift; it is the only ambient motion in fair weather and does more
    // for "alive" than anything else. Whole pixels only.
    if (kind !== "clear") {
      const windMul = 1 + ((weather && weather.wind_kph) || 0) / 25;
      ctx.fillStyle = kind === "fog" ? "#c7dcd0" : night ? "#3a4466" : C.cloud;
      for (const [oy, cw, rate] of [[4, 22, 26], [11, 28, 15], [19, 18, 34]]) {
        const drift = Math.floor(now / (rate / windMul)) % (WINDOW.w + 34);
        for (let i = 0; i < cw; i++) {
          const px = WINDOW.x + ((i + drift) % (WINDOW.w + 34)) - 17;
          if (px < WINDOW.x || px >= WINDOW.x + WINDOW.w) continue;
          ctx.fillRect(px, WINDOW.y + oy, 1, i === 0 || i === cw - 1 ? 1 : 2);
        }
      }
    }

    drawWeather(now, dt);
    const spill = lightning(now);

    ctx.fillStyle = C.frame;                       // glazing bars, over everything
    ctx.fillRect(WINDOW.x + ((WINDOW.w / 2) | 0), WINDOW.y, 1, WINDOW.h);
    ctx.fillRect(WINDOW.x, WINDOW.y + ((WINDOW.h / 2) | 0), WINDOW.w, 1);
    ctx.fillStyle = C.wallCap;
    ctx.fillRect(WINDOW.x - 3, WINDOW.y + WINDOW.h + 3, WINDOW.w + 6, 2);   // sill

    if (spill > 0) {   // light entering the room, not the screen blinking
      ctx.globalCompositeOperation = "lighter";
      ctx.globalAlpha = spill * 0.35; ctx.fillStyle = "#fdcbb0";
      ctx.fillRect(WINDOW.x - 6, WALL_H, WINDOW.w + 12, 46);
      ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over";
    }
    if (weather && weather.stale) {
      ctx.fillStyle = C.dim; ctx.font = "5px ui-monospace, monospace"; ctx.textAlign = "left";
      ctx.fillText("?", WINDOW.x + WINDOW.w + 5, WINDOW.y + 7);
    }
  }

  // --- Fred ----------------------------------------------------------------
  // What he is doing, not just where he is: "sleep" | "walk" | "inspect" |
  // "type" | "fight". The activity survives arrival, so he keeps doing the thing
  // until something else happens or the idle timer sends him to bed.
  const fred = {
    x: BED.x + 8, y: BED.y + 30, dir: "down", flip: false,
    dist: 0, asleep: true, activity: "sleep", say: "", sayUntil: 0,
    lastEvent: Date.now(),
  };
  const DESK_SEAT = {x: BUNK.x + 24, y: BUNK.y + 92};   // chair, below the terminal
  const COOK_SPOT = {x: KITCHEN.x + 8, y: KITCHEN.y + KITCHEN.h + 12};
  const READ_SPOT = {x: CHAIR.x + 7, y: CHAIR.y + CHAIR.h + 2};
  let unacked = 0, tl = null, walking = false;
  // A rack burns while it has an unacked escalation -- which is what "that
  // server is on fire" actually means. Cleared by an ack or an all-clear.
  const burning = new Set();
  const IDLE_MS = 90000;
  let nowMs = 0;              // current frame time, for the fire's noise
  const pendingGlow = [];     // fire glows, collected during the sorted pass
  const pendingSteam = [];    // steam from water landing, drawn with the glows

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
      onArrive && onArrive();
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
      if (onArrive) onArrive();
    });
  }

  function goSleep() {
    fred.say = "";
    walk(BED.x + 8, BED.y + 30, null);
    if (tl) tl.call(() => { fred.asleep = true; fred.activity = "sleep"; });
    else { fred.asleep = true; }
  }


  // --- his day ---------------------------------------------------------------
  // Idle is not one behaviour. Given nothing to do he keeps hours: asleep at
  // night, coffee and the terminal in the morning, cooking around meals, a book
  // in the evening. Work always interrupts -- an event pulls him out of any of
  // these -- so the routine only decides what "nothing to do" looks like.
  function routineFor(d) {
    const h = d.getHours() + d.getMinutes() / 60;
    if (h >= 23 || h < 6.5) return "sleep";
    if (h < 8.5)            return "type";    // coffee, at the terminal
    if (h >= 12 && h < 13)  return "cook";
    if (h >= 17.5 && h < 19) return "cook";
    if (h >= 20)            return "read";
    return "type";
  }

  const ROUTINE_SPOT = {
    sleep: () => ({x: BED.x + 8, y: BED.y + 30}),
    type:  () => ({x: DESK_SEAT.x, y: DESK_SEAT.y}),
    cook:  () => ({x: COOK_SPOT.x, y: COOK_SPOT.y}),
    read:  () => ({x: READ_SPOT.x, y: READ_SPOT.y}),
  };

  function goIdle() {
    const want = routineFor(new Date());
    const spot = ROUTINE_SPOT[want]();
    fred.say = "";
    fred.asleep = false;          // he has to get up before he can go anywhere
    walk(spot.x, spot.y, () => {
      fred.activity = want;
      fred.asleep = (want === "sleep");
    });
  }

  // Reading: seated side-on with a book held up. Cooking reuses the up pose
  // with a stirring arm, because at 12px a stir is an arm and nothing else.
  const READ = [
    ["............", "...hhhh.....", "..hhhhhh....", "..hhssss....",
     "..hhsEss....", "..hhssss....", "...ssss.....", "..kCCCCk....",
     "..CCCCCCs...", "..CCCCCCs...", "..kCCCCk....", "..PPPPPP....",
     "..PPPP......", "..bbb......."],
    ["............", "...hhhh.....", "..hhhhhh....", "..hhssss....",
     "..hhssss....", "..hhsEss....", "...ssss.....", "..kCCCCk....",
     "..CCCCCCs...", "..CCCCCCs...", "..kCCCCk....", "..PPPPPP....",
     "..PPPP......", "..bbb......."],
  ];

  function drawBook(fx, fy, now) {
    const flip = Math.floor(now / 2600) % 2;
    ctx.fillStyle = C.rackEdge; ctx.fillRect(fx + 7, fy - 10, 5, 5);
    ctx.fillStyle = flip ? C.book : C.bookB; ctx.fillRect(fx + 8, fy - 9, 3, 3);
  }

  function drawPot(now) {
    const k = KITCHEN;
    ctx.fillStyle = C.rackEdge; ctx.fillRect(k.x + 4, k.y + 8, 8, 2);
    ctx.fillStyle = C.pot;      ctx.fillRect(k.x + 4, k.y + 9, 8, 5);
    // steam, only while something is on the hob
    ctx.fillStyle = C.steam; ctx.globalAlpha = 0.5;
    for (let i = 0; i < 3; i++) {
      const t = ((now / 620 + i * 0.33) % 1);
      ctx.fillRect(k.x + 5 + i * 2 + Math.round(Math.sin(now / 400 + i) * 1),
                   Math.round(k.y + 7 - t * 11), 1, 1);
    }
    ctx.globalAlpha = 1;
  }

  // --- render --------------------------------------------------------------
  let prevX = fred.x, prevY = fred.y;

  function draw(now) {
    if (!bg) paintBackground();
    ctx.drawImage(bg, 0, 0);
    drawWindow(now, dtMs);
    drawSign(now);
    drawClock(now);
    if (fred.activity === "cook" && !walking) drawPot(now);
    nowMs = now;
    pendingGlow.length = 0;
    pendingSteam.length = 0;

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
      ctx.fillStyle = "rgba(15,15,24,.45)";
      ctx.fillRect(Math.round(fred.x) - 4, Math.round(fred.y) - 1, 9, 2);

      // Standing still is the least informative thing he can do, so every
      // arrival hands off to a pose that says what he is actually doing.
      if (!walking && fred.activity === "read") {
        blit(READ[Math.floor(now / 1500) % 2], fred.x - 6, fred.y - 14, false);
        drawBook(fred.x - 6, fred.y, now);
      } else if (!walking && fred.activity === "cook") {
        const stir = Math.floor(now / 300) % 2;
        blit(SPR.up, fred.x - 6, fred.y - 18 - stir, false);
      } else if (!walking && fred.activity === "type") {
        blit(SIT[Math.floor(now / 190) % 2], fred.x - 6, fred.y - 12, false);
      } else if (!walking && fred.activity === "inspect") {
        blit(INSPECT[Math.floor(now / 260) % 2], fred.x - 6, fred.y - 14, false);
      } else if (!walking && fred.activity === "fight") {
        // Facing the fire with a bucket. He braces on the throw, which is the
        // only frame that reads as effort at 12px wide.
        const target = racks.find(r => burning.has(r.node) && fightingHere(r));
        const seed = target ? target.x * 31 + target.base : 0;
        const p = throwPhase(now, seed);
        const brace = (p >= RELEASE && p < RELEASE + 0.14) ? 1 : 0;
        blit(SPR.up, fred.x - 6, fred.y - 18 + brace, false);
        drawBucket(fred.x - 6, fred.y, now, seed);
        if (target) {
          drawWater(fred.x - 6, fred.y,
                    target.x + RACK_W / 2, target.base - RACK_H + 2, now, seed);
        }
      } else {
        const key = (fred.dir === "left" || fred.dir === "right") ? "side" : fred.dir;
        const rows = apart ? SPR[key].slice(0, 14).concat(LEGS_APART[key]) : SPR[key];
        blit(rows, fred.x - 6, fred.y - 18 + (apart ? -1 : 0), fred.flip);
      }
    }

    for (const g of pendingGlow) fireGlow(g[0], g[1], g[2], now, g[3], g[4]);
    for (const s2 of pendingSteam) drawSteam(s2[0], s2[1], now, s2[2]);

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

    if (fred.asleep && fred.activity === "sleep" && Math.floor(now / 900) % 2) {
      ctx.fillStyle = C.dim; ctx.font = "6px ui-monospace, monospace"; ctx.textAlign = "left";
      ctx.fillText("z", BED.x + BED.w + 3, BED.y + 12);
    }

    // the bunk terminal, always on
    if (TERMINAL) {
      const t = TERMINAL, busy = fred.activity === "type" && !walking;
      // Idling it just flickers; while he is actually working it scrolls lines,
      // so the screen tells you the same thing his pose does from across the room.
      ctx.fillStyle = (Math.floor(now / 140) % 5) === 0 ? "#3a5878" : "#2f4a6a";
      ctx.fillRect(t.x, t.y, t.w, t.h);
      if (busy) {
        for (let i = 0; i < 4; i++) {
          const row = (i + Math.floor(now / 220)) % 5;
          const len = 2 + Math.floor(hash2(i * 13, Math.floor(now / 220)) * (t.w - 4));
          ctx.fillStyle = i === 0 ? "#a8e6a0" : "#63c74d";
          ctx.fillRect(t.x + 1, t.y + 1 + row, len, 1);
        }
      }
      ctx.globalCompositeOperation = "lighter"; ctx.globalAlpha = busy ? .22 : .12;
      ctx.fillStyle = busy ? "#63c74d" : "#4a7ab0";
      ctx.fillRect(t.x - 3, t.y - 3, t.w + 6, t.h + 6);
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
  let running = true, lastT = 0, dtMs = 33;
  function frame(now) {
    requestAnimationFrame(frame);
    if (!running || now - lastT < MIN_DT) return;
    // Derived from the rAF timestamp, not assumed: on a 144Hz display a fixed
    // step would run the weather 2.4x too fast. Clamped so a backgrounded tab
    // does not teleport every raindrop on return.
    dtMs = Math.min(now - lastT, 120);
    lastT = now;
    if (A) A.engine.update();          // advance tweens exactly once, then draw

    // Going to bed is latched rather than gated on !walking: the old form meant
    // that if a walk ever failed to signal completion -- a cancelled timeline,
    // or the loop being paused mid-stride while the tab sat on another view --
    // he stayed on his feet indefinitely with nothing to retry it.
    // Idle behaviour is re-decided against the clock, not latched once. Gating
    // this on "not asleep" meant that once he went to bed nothing ever
    // reconsidered, so he slept straight through the following day.
    const idle = Date.now() - fred.lastEvent;
    if (idle > IDLE_MS && !walking) {
      const want = routineFor(new Date());
      if (fred.activity !== want) {
        goIdle();
      }
    } else if (idle > IDLE_MS * 4 && walking && !tl) {
      // Stranded mid-routine with no timeline to finish the walk.
      const spot = ROUTINE_SPOT[routineFor(new Date())]();
      fred.x = spot.x; fred.y = spot.y; walking = false;
      fred.activity = routineFor(new Date());
      fred.asleep = fred.activity === "sleep";
    }
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
      : e.kind === "thinking" ? (msg || "thinking")
      : e.kind === "reminder" ? "still: " + (e.subject || "")
      : e.kind === "resolved" ? "all clear: " + (e.subject || "")
      : e.kind === "update" ? "updating " + (e.subject || "")
      : e.kind === "backup" ? "checking backups"
      : e.kind === "finding" ? (e.subject ? e.subject + ": " : "") + msg
      : (msg || "walking the floor");
    fred.sayUntil = performance.now() + 12000;


    // "thinking" is the warden telling us inference has started. It is the one
    // slow thing this agent does, and it belongs at the desk, not at a rack.
    if (e.kind === "thinking") {
      fred.activity = "walk";
      walk(DESK_SEAT.x, DESK_SEAT.y, () => { fred.activity = "type"; });
      return;
    }

    const r = rackFor(e.subject);
    if (r) {
      if (e.severity === "escalate") burning.add(r.node);
      if (e.kind === "resolved") burning.delete(r.node);
      const next = burning.has(r.node) ? "fight"
                 : (e.kind === "update" || e.kind === "backup") ? "inspect"
                 : e.kind === "resolved" ? "idle" : "inspect";
      fred.activity = "walk";
      walk(r.x + RACK_W / 2, r.base === ROW_A_Y ? AISLE_MAIN : AISLE_FRONT,
           () => { fred.activity = next; });
    }
    // Cluster-scope events ("patrol pass complete" ends every single pass) name
    // no rack. Sending him to a fixed spot for those parked him in the dead
    // centre of the room after every patrol, which is where he was found
    // standing. He says his line from wherever he is and the idle timer takes
    // him home.
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
      const r = await fetch("/api/weather");
      weather = r.ok ? await r.json() : null;   // 404 simply means unconfigured
    } catch { weather = null; }

    try {
      const d = await (await fetch("/api/events?limit=60")).json();
      const evs = d.events || [];
      const acked = new Set(evs.filter(e => e.kind === "ack").map(e => e.ref));
      const live = evs.filter(e => e.severity === "escalate" && !acked.has(e.id));
      // Count distinct subjects, not messages. A long-open escalation is both a
      // finding and a growing pile of reminders about that same finding, and
      // "4 to ack" should mean four problems rather than four sentences.
      unacked = new Set(live.map(e => e.subject)).size;
      // Rebuild from scratch so an ack or an all-clear that happened while this
      // tab was closed actually puts the fire out.
      burning.clear();
      const cleared = new Set(evs.filter(e => e.kind === "resolved").map(e => e.subject));
      for (const e of live) {
        const r = rackFor(e.subject);
        if (r && !cleared.has(e.subject)) burning.add(r.node);
      }
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
      if (e.kind === "ack") {
        unacked = Math.max(0, unacked - 1);
        const r = rackFor(e.subject);
        if (r) burning.delete(r.node);
        return;
      }
      react(e);
    } catch (err) {
      // Swallowing this kept the stream alive but left Fred frozen with no
      // signal anywhere — the symptom looked like "he just stopped walking".
      console.error("room: failed to handle event", err);
    }
  };

  // test hook: age the idle clock without waiting it out in real time
  if (typeof window !== 'undefined') window.__forceIdle = () => { fred.lastEvent = 0; };
  load().then(() => requestAnimationFrame(frame));
  setInterval(load, 60000);
})();
