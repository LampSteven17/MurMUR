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

  // 480x300 — same 8:5 aspect, 2.25x the pixels. The extra density is spent on
  // shading ramps and surface detail rather than on a bigger room: the previous
  // pass was flat because every surface was two-tone, not because it was small.
  const W = 480, H = 300, TILE = 24;
  cvs.width = W; cvs.height = H;
  const ctx = cvs.getContext("2d", {alpha: false});
  ctx.imageSmoothingEnabled = false;

  const A = window.anime || null;   // optional: scene still runs without it

  // --- palette -------------------------------------------------------------
  // Twelve muted cool structural colours; saturated colour is reserved for
  // emissives, so the status lights are the only vivid pixels in the frame.
  // Fred is warm, which separates him from the room without competing.
  const C = {
    // Ramps, not pairs. Each surface gets 4-5 steps whose highlights shift warm
    // and whose shadows shift cool -- brightness alone reads as plastic. Light
    // is from the upper left, consistently, everywhere in the scene.
    ink:"#0b0a10", deep:"#15121f",

    wall1:"#1b1a2b", wall2:"#242439", wall3:"#2e2f49", wall4:"#3a3c5c", wall5:"#4a4d70",
    flr1:"#1e1f31", flr2:"#262940", flr3:"#2f334e", flr4:"#3a3f5e", flr5:"#4a5070",
    seam:"#171826", scuff:"#3f4566",

    stl1:"#12111b", stl2:"#242639", stl3:"#343a55", stl4:"#4a5273", stl5:"#646d92", stl6:"#8792b5",
    vent:"#0f0e17", rail:"#5a6488", handle:"#7c86ab",

    wood1:"#2b1f18", wood2:"#4a3524", wood3:"#6b4c33", wood4:"#8a6544", wood5:"#a8825c",
    cloth1:"#3a4a6b", cloth2:"#55688f", cloth3:"#7d90b8", cloth4:"#a9bad9",

    on:"#4ade5e", onDim:"#1e5e2c", bad:"#f2555a", badDim:"#6b1f24", off:"#2a2f45",
    fCore:"#fff6c2", fHot:"#ffe066", fMid:"#feae34", fEdge:"#e43b44",
    fDark:"#a02c2c", smoke:"#4a5878",
    water:"#4aa8e8", waterLit:"#9fdcff", bucket:"#7a6a58", bucketRim:"#a89680",
    steam:"#c0cbdc",
    counter:"#3b3550", counterTop:"#5c5578", counterLit:"#7a7398", hob:"#1a1622", pot:"#8792b5",
    chairA:"#3a2f46", chairB:"#4e4060", chairC:"#66557c",
    book:"#a8524a", bookB:"#4a6ea8", bookC:"#6ea85a",
    skyDay:"#6f9fd0", skyDusk:"#8a6a8c", skyNight:"#141d33",
    sun:"#ffe9a8", moon:"#dfe6f2", star:"#c0cbdc", cloud:"#8ea6c4",
    rain:"#7fb0e0", snow:"#e8f0ff", frame:"#2a2436", frameLit:"#443c55",
    neonHot:"#ff5cc8", neonCore:"#ffd6f2", neonDim:"#8a3a6e", neonDead:"#33263a",
    tube:"#241c2c", cable:"#3d3550", cableLit:"#544a68",
    ledOn:"#f2555a", ledDim:"#4a1d22", bezel:"#16131f", bezelTop:"#2c2636",
    // Fred: warm against a cool room, with enough steps to shade at 18px wide
    hair1:"#2b1c12", hair2:"#4a3526", hair3:"#6b4d36",
    skin1:"#b57a52", skin2:"#e0a578", skin3:"#f2c9a0",
    shirt1:"#5e3a20", shirt2:"#8a5730", shirt3:"#b0764a",
    pant1:"#232840", pant2:"#343c5c", boot:"#15121f",
    txt:"#c9d4e8", dim:"#5a6488", rug:"#2a2f47", rugLit:"#39405e",
    quilt:"#55688f", pillow:"#a9bad9", bedFrame:"#4a3524", bedTop:"#6b4c33",
    rackEdge:"#0d0c14", rackTop:"#646d92", rackFace:"#343a55", rackLit:"#4a5273",
    wallCap:"#4a4d70", wall:"#242439", floorA:"#262940", floorB:"#2f334e",
    dark:"#0d0c14",
  };
  // --- character sprites: 12 wide, 18 tall, one char per pixel -------------
  // Two leg positions per direction. The stride is carried by swapping the legs
  // AND lifting the whole sprite 1px — animating legs alone reads as shuffling.
  const SPR = {
    // 18x27. Every row is exactly 18 characters and the face is built on a
    // single centre line between columns 8 and 9 -- the previous head had its
    // eyes a pixel left of centre, which is why he looked wrong rather than
    // stylised. Eyes are 2x2 with a brow above; anything larger reads as bulging.
    down: [
      "......hhhhhh......","....hhhhhhhhhh....","...thhhhhhhhhhH...","..thhhhhhhhhhhhH..",
      "..thhSSSSSSSShhH..","..thSSSSSSSSSShH..","..thSSSSSSSSSShH..","..thSSEESSEESShH..",
      "..thSSEESSEESShH..","..thSSSSSSSSSShH..","..thSSSSddSSSShH..","...hSSSSSSSSSSh...",
      "......SSSSSS......","...kCCCCCCCCCCk...","..kLCCCCCCCCCCck..","..SLCCCCCCCCCCcd..",
      "..SLCCCCCCCCCCcd..","..SLCCCCCCCCCCcd..","..kLCCCCCCCCCCck..","...LCCCCCCCCCCc...",
      "...LCCCCCCCCCCc...","...PPPPPPPPPPPP...","...PPPPPPPPPPPP...","...PPPPP..PPPPP...",
      "...PPPPP..PPPPP...","...bbbb....bbbb...","...bbbb....bbbb...",
    ],
    up: [
      "......hhhhhh......","....hhhhhhhhhh....","...thhhhhhhhhhH...","..thhhhhhhhhhhhH..",
      "..thhhhhhhhhhhhH..","..thhhhhhhhhhhhH..","..thhhhhhhhhhhhH..","..thhhhhhhhhhhhH..",
      "..thhhhhhhhhhhhH..","..thhhhhhhhhhhhH..","..thhhhhhhhhhhhH..","...hhhhhhhhhhhh...",
      "......SSSSSS......","...kCCCCCCCCCCk...","..kLCCCCCCCCCCck..","..SLCCCCCCCCCCcd..",
      "..SLCCCCCCCCCCcd..","..SLCCCCCCCCCCcd..","..kLCCCCCCCCCCck..","...LCCCCCCCCCCc...",
      "...LCCCCCCCCCCc...","...PPPPPPPPPPPP...","...PPPPPPPPPPPP...","...PPPPP..PPPPP...",
      "...PPPPP..PPPPP...","...bbbb....bbbb...","...bbbb....bbbb...",
    ],
    // Profile: the head is narrower, the far arm is hidden, and one eye sits
    // forward of centre. Drawn facing right; left is the mirror.
    side: [
      ".....hhhhhh.......","...hhhhhhhhhh.....","..thhhhhhhhhhH....","..thhhhhhhhhhhH...",
      "..thhSSSSSSSSh....","..thSSSSSSSSSS....","..thSSSSSSSSSS....","..thSSSSEESSS.....",
      "..thSSSSEESSS.....","..thSSSSSSSSS.....","..thSSSSSddSS.....","...hSSSSSSSS......",
      ".....SSSSS........","...kCCCCCCCCk.....","..kLCCCCCCCCCk....","..SLCCCCCCCCCd....",
      "..SLCCCCCCCCCd....","..SLCCCCCCCCCd....","..kLCCCCCCCCCk....","...LCCCCCCCCc.....",
      "...LCCCCCCCCc.....","...PPPPPPPPPP.....","...PPPPPPPPPP.....","...PPPPP..PPP.....",
      "...PPPPP..PPP.....","...bbbb...bbb.....","...bbbb...bbb.....",
    ],
  };
  // Legs apart: the final six rows swap, and the whole sprite lifts 1px. Moving
  // legs alone reads as a shuffle; lifting the body is what reads as a stride.
  const LEGS_APART = {
    down: ["...PPPPPPPPPPPP...","..PPPPP......PPPP.","..PPPP........PPP.",
           "..bbbb........bbbb","..bbbb........bbbb","..................",],
    up:   ["...PPPPPPPPPPPP...","..PPPPP......PPPP.","..PPPP........PPP.",
           "..bbbb........bbbb","..bbbb........bbbb","..................",],
    side: ["...PPPPPPPPPP.....","..PPPPP...PPPP....","..PPPP.....PPP....",
           "..bbbb.....bbb....","..bbbb.....bbb....","..................",],
  };
  // Asleep: a dedicated pose, lying under the quilt with the head on the pillow.
  // 30x14, head at the left, and the quilt line does the work of a body.
  const SLEEP = [
    "........hhhhhh................","......hhhhhhhhhh..............",
    ".....thhSSSSSShH..............",".....thSSSSSSSSH..............",
    ".....thSS-SS-SSH..............",".....thSSSSSSSSH..............",
    "......hSSSSSSH................","......QQQQQQQQQQQQQQQQQQQQ....",
    ".....QQQQQQQQQQQQQQQQQQQQQQ...",".....QQQQQQQQQQQQQQQQQQQQQQ...",
    ".....qQQQQQQQQQQQQQQQQQQQQq...",".....qqQQQQQQQQQQQQQQQQQQqq...",
    "......qqqqqqqqqqqqqqqqqqqq....","..............................",
  ];
  const PX = {
    h:C.hair2, H:C.hair1, t:C.hair3,
    s:C.skin2, S:C.skin3, d:C.skin1, E:C.ink,
    C:C.shirt2, L:C.shirt3, c:C.shirt1,
    P:C.pant2, p:C.pant1, b:C.boot, k:C.ink,
    Q:C.quilt, q:C.cloth1, "-":C.ink,
  };

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
  const WALL_H = 70, FLOOR_B = 288;
  const BUNK = {x:9, y:76, w:154, h:206};
  // Studio interior is x 9..163, y 76..282. Everything below is placed against a
  // wall with a clear gap to its neighbours -- the previous pass had the shelf
  // sitting inside the desk.
  const BED   = {x:14,  y:84,  w:42, h:54};   // top-left corner
  const DESK  = {x:34,  y:206, w:70, h:30};   // bottom-left, clear of the shelf
  const WINDOW  = {x:36, y:10, w:96, h:50};
  const KITCHEN = {x:96, y:84, w:62, h:42};   // top-right, against both walls
  const CHAIR   = {x:118, y:206, w:34, h:34}; // bottom-right
  const SHELF   = {x:14,  y:150, w:14, h:46}; // left wall, between bed and desk
  const DOOR = {y0:238, y1:280};
  const RACK_W = 27, RACK_H = 48, TOP_H = 9;
  const ROW_A_Y = 156, ROW_B_Y = 252;
  const RACK_X = [196, 264, 332, 400];
  const AISLE_MAIN = 192, AISLE_FRONT = 276;
  const GAPS = [183, 243, 312, 381, 447];
  const DOOR_X = BUNK.x + BUNK.w + 18;
  const SIGN  = {x:200, y:14, w:87, h:21};
  const CLOCK = {x:372, y:14, w:52, h:24};

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
          x: r.x + RACK_W - 7, y: top + 12 + s * 6,
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

    // --- back wall: panelled, with conduit. Flat walls are what made the last
    // pass read as a diagram; the panel seams and the pipe run give the eye
    // something to measure the room against.
    b.fillStyle = C.wall2; b.fillRect(0, 0, W, WALL_H);
    for (let x = 0; x < W; x += 40) {
      b.fillStyle = C.wall3; b.fillRect(x, 2, 38, WALL_H - 6);
      b.fillStyle = C.wall4; b.fillRect(x, 2, 38, 1);
      b.fillStyle = C.wall1; b.fillRect(x + 37, 3, 1, WALL_H - 7);
      b.fillStyle = C.wall1; b.fillRect(x, WALL_H - 4, 38, 1);
    }
    b.fillStyle = C.wall5; b.fillRect(0, WALL_H - 3, W, 3);   // lit top of the skirting
    b.fillStyle = C.wall1; b.fillRect(0, WALL_H, W, 2);
    // conduit run along the wall, with brackets
    b.fillStyle = C.stl2; b.fillRect(0, 8, W, 3);
    b.fillStyle = C.stl4; b.fillRect(0, 8, W, 1);
    for (let x = 14; x < W; x += 46) { b.fillStyle = C.stl1; b.fillRect(x, 7, 2, 5); }

    // --- floor: raised panels with worn edges and a lit upper-left corner per
    // tile, so the grid reads as physical panels rather than a checkerboard.
    for (let y = WALL_H + 2; y < FLOOR_B; y += TILE) {
      for (let x = 0; x < W; x += TILE) {
        const h = hash2(x, y);
        b.fillStyle = h > 0.55 ? C.flr3 : C.flr2;
        b.fillRect(x, y, TILE, TILE);
        b.fillStyle = C.flr4; b.fillRect(x + 1, y + 1, TILE - 2, 1);
        b.fillStyle = C.flr1; b.fillRect(x, y + TILE - 1, TILE, 1);
        b.fillRect(x + TILE - 1, y, 1, TILE);
        if (h > 0.86) {                      // wear, clustered not scattered
          b.fillStyle = C.scuff;
          for (let k = 0; k < 3; k++)
            b.fillRect(x + 4 + ((h * 13 + k * 3) | 0) % (TILE - 6), y + 5 + (k * 4), 2, 1);
        }
        if (h < 0.08) { b.fillStyle = C.flr5; b.fillRect(x + 3, y + 3, 2, 2); }  // stud
      }
    }
    b.fillStyle = C.deep; b.fillRect(0, FLOOR_B, W, H - FLOOR_B);
    b.fillStyle = C.wall1; b.fillRect(0, FLOOR_B, W, 2);

    // --- cable runs snaking between the rack rows, Noita-ish clutter that also
    // happens to be what a real floor like this looks like
    signHardware(b);
    studio(b);
    lightPools(b);
  }

  function signHardware(b) {
    b.fillStyle = C.tube;
    b.fillRect(SIGN.x - 3, SIGN.y - 2, SIGN.w + 6, SIGN.h + 3);
    b.fillStyle = C.stl2; b.fillRect(SIGN.x - 3, SIGN.y - 2, SIGN.w + 6, 2);
    b.fillStyle = C.cableLit; b.fillRect(SIGN.x - 6, SIGN.y - 5, SIGN.w + 12, 3);
    b.fillStyle = C.cable;
    b.fillRect(SIGN.x - 6, SIGN.y - 5, 3, 6);
    b.fillRect(SIGN.x + SIGN.w + 3, SIGN.y - 5, 3, 6);

    const drape = (x0, x1, sag, cut) => {
      const span = x1 - x0;
      for (let i = 0; i <= span; i++) {
        const t = i / span;
        const yy = SIGN.y + SIGN.h + Math.round(Math.sin(t * Math.PI) * sag);
        b.fillStyle = C.cable; b.fillRect(x0 + i, yy, 1, 3);
        if (i % 9 === 4) { b.fillStyle = C.cableLit; b.fillRect(x0 + i, yy, 1, 1); }
        if (i === span && cut) {
          b.fillStyle = C.cable;
          for (let k = 1; k <= 9; k++) b.fillRect(x0 + span + (k > 5 ? 1 : 0), yy + k, 1, 1);
          b.fillStyle = C.bad; b.fillRect(x0 + span + 1, yy + 10, 2, 2);   // bare copper
        }
      }
    };
    drape(SIGN.x + 6, SIGN.x + 39, 10, false);
    drape(SIGN.x + 45, SIGN.x + 72, 7, true);

    b.fillStyle = C.rackEdge; b.fillRect(CLOCK.x - 2, CLOCK.y - 2, CLOCK.w + 4, CLOCK.h + 4);
    b.fillStyle = C.bezel;    b.fillRect(CLOCK.x, CLOCK.y, CLOCK.w, CLOCK.h);
    b.fillStyle = C.bezelTop; b.fillRect(CLOCK.x, CLOCK.y, CLOCK.w, 2);
    b.fillStyle = C.stl1;     b.fillRect(CLOCK.x, CLOCK.y + CLOCK.h - 2, CLOCK.w, 2);
    // display recessed with an even margin, so the digits are not floating in
    // the top-left of an oversized panel
    b.fillStyle = C.ink;      b.fillRect(CLOCK.x + 4, CLOCK.y + 4, CLOCK.w - 8, CLOCK.h - 8);
    b.fillStyle = C.cable;    b.fillRect(CLOCK.x + CLOCK.w - 10, CLOCK.y + CLOCK.h + 2, 2, 9);
  }

  function studio(b) {
    // rug, with a border and a woven centre -- Stardew's trick of making the
    // floor of a living space read differently from the floor of a workspace
    b.fillStyle = C.rug; b.fillRect(BUNK.x, BUNK.y, BUNK.w, BUNK.h);
    b.fillStyle = C.rugLit; b.fillRect(BUNK.x + 3, BUNK.y + 3, BUNK.w - 6, 2);
    for (let y = BUNK.y + 8; y < BUNK.y + BUNK.h - 4; y += 6) {
      b.fillStyle = C.rugLit; b.fillRect(BUNK.x + 5, y, BUNK.w - 10, 1);
    }
    b.fillStyle = C.wall1;
    b.fillRect(BUNK.x + BUNK.w, BUNK.y - 6, 4, DOOR.y0 - BUNK.y + 6);
    b.fillRect(BUNK.x + BUNK.w, DOOR.y1, 4, FLOOR_B - DOOR.y1);
    b.fillRect(BUNK.x - 4, BUNK.y - 6, 4, BUNK.h + 6);
    b.fillStyle = C.wall4; b.fillRect(BUNK.x + BUNK.w, BUNK.y - 6, 4, 2);

    // bed: frame, mattress, quilt with a fold, two pillows
    b.fillStyle = C.rackEdge; b.fillRect(BED.x - 2, BED.y - 2, BED.w + 4, BED.h + 4);
    b.fillStyle = C.wood2;    b.fillRect(BED.x, BED.y, BED.w, BED.h);
    b.fillStyle = C.wood4;    b.fillRect(BED.x, BED.y, BED.w, 3);
    b.fillStyle = C.wood3;    b.fillRect(BED.x + 2, BED.y + 3, BED.w - 4, BED.h - 9);
    b.fillStyle = C.pillow;   b.fillRect(BED.x + 4, BED.y + 4, BED.w - 8, 10);
    b.fillStyle = C.cloth4;   b.fillRect(BED.x + 4, BED.y + 4, BED.w - 8, 2);
    b.fillStyle = C.quilt;    b.fillRect(BED.x + 3, BED.y + 16, BED.w - 6, BED.h - 21);
    b.fillStyle = C.cloth3;   b.fillRect(BED.x + 3, BED.y + 16, BED.w - 6, 2);
    b.fillStyle = C.cloth1;   b.fillRect(BED.x + 3, BED.y + BED.h - 7, BED.w - 6, 2);
    for (let i = 1; i < 4; i++) {   // quilt seams, so it is not one flat field
      b.fillStyle = C.cloth1;
      b.fillRect(BED.x + 3, BED.y + 16 + i * 8, BED.w - 6, 1);
    }

    // kitchen: counter, two hobs with rings, a sink, a splashback
    b.fillStyle = C.rackEdge; b.fillRect(KITCHEN.x - 2, KITCHEN.y - 2, KITCHEN.w + 4, KITCHEN.h + 4);
    b.fillStyle = C.counter;    b.fillRect(KITCHEN.x, KITCHEN.y, KITCHEN.w, KITCHEN.h);
    b.fillStyle = C.counterTop; b.fillRect(KITCHEN.x, KITCHEN.y, KITCHEN.w, 5);
    b.fillStyle = C.counterLit; b.fillRect(KITCHEN.x, KITCHEN.y, KITCHEN.w, 1);
    for (const hx of [KITCHEN.x + 7, KITCHEN.x + 24]) {
      b.fillStyle = C.hob;  b.fillRect(hx, KITCHEN.y + 12, 12, 12);
      b.fillStyle = C.stl3; b.fillRect(hx + 2, KITCHEN.y + 14, 8, 1);
      b.fillRect(hx + 2, KITCHEN.y + 21, 8, 1);
      b.fillRect(hx + 1, KITCHEN.y + 16, 1, 4);
      b.fillRect(hx + 10, KITCHEN.y + 16, 1, 4);
    }
    b.fillStyle = C.stl2; b.fillRect(KITCHEN.x + 40, KITCHEN.y + 11, 13, 16);
    b.fillStyle = C.stl4; b.fillRect(KITCHEN.x + 41, KITCHEN.y + 12, 11, 2);
    b.fillStyle = C.stl5; b.fillRect(KITCHEN.x + 46, KITCHEN.y + 6, 1, 6);   // tap
    b.fillRect(KITCHEN.x + 46, KITCHEN.y + 6, 4, 1);
    b.fillStyle = C.wall1; b.fillRect(KITCHEN.x + 3, KITCHEN.y + KITCHEN.h - 6, KITCHEN.w - 6, 1);
    // cupboard doors under the counter, so the block is not one flat slab
    for (const dx of [4, 22, 40]) {
      b.fillStyle = C.counterTop; b.fillRect(KITCHEN.x + dx, KITCHEN.y + 30, 14, 11);
      b.fillStyle = C.counter;    b.fillRect(KITCHEN.x + dx + 1, KITCHEN.y + 31, 12, 9);
      b.fillStyle = C.counterLit; b.fillRect(KITCHEN.x + dx + 5, KITCHEN.y + 34, 4, 1);
    }

    // fridge: tall, its own body, freezer door on top and a long handle
    const F = {x: 134, y: KITCHEN.y + KITCHEN.h + 8, w: 24, h: 52};   // right wall
    b.fillStyle = C.rackEdge; b.fillRect(F.x - 2, F.y - 2, F.w + 4, F.h + 4);
    b.fillStyle = C.stl3;     b.fillRect(F.x, F.y, F.w, F.h);
    b.fillStyle = C.stl5;     b.fillRect(F.x, F.y, F.w, 3);
    b.fillStyle = C.stl4;     b.fillRect(F.x, F.y + 3, 3, F.h - 3);
    b.fillStyle = C.stl2;     b.fillRect(F.x + F.w - 3, F.y + 3, 3, F.h - 3);
    b.fillStyle = C.stl1;     b.fillRect(F.x + 2, F.y + 17, F.w - 4, 2);   // door split
    b.fillStyle = C.handle;
    b.fillRect(F.x + F.w - 8, F.y + 8, 2, 7);
    b.fillRect(F.x + F.w - 8, F.y + 23, 2, 22);
    b.fillStyle = C.bad;  b.fillRect(F.x + 5, F.y + 6, 3, 2);              // a magnet
    b.fillStyle = C.on;   b.fillRect(F.x + 10, F.y + 6, 3, 2);
    b.fillStyle = C.wood5; b.fillRect(F.x + 6, F.y + 24, 8, 6);            // a note
    b.fillStyle = C.stl1; b.fillRect(F.x + 2, F.y + F.h - 3, F.w - 4, 3);

    // desk + monitor + mug + chair
    const D = DESK;
    b.fillStyle = C.rackEdge; b.fillRect(D.x - 2, D.y - 2, D.w + 4, D.h + 4);
    b.fillStyle = C.wood2;    b.fillRect(D.x, D.y, D.w, D.h);
    b.fillStyle = C.wood4;    b.fillRect(D.x, D.y, D.w, 4);
    // monitor: outer shell, inset bezel, then the screen. The screen is inset
    // on all four sides so it reads as recessed glass rather than a panel with
    // a stripe above it.
    const M = {x: D.x + 14, y: D.y - 22, w: 30, h: 21};
    b.fillStyle = C.stl1; b.fillRect(M.x - 1, M.y - 1, M.w + 2, M.h + 2);
    b.fillStyle = C.stl3; b.fillRect(M.x, M.y, M.w, M.h);
    b.fillStyle = C.stl4; b.fillRect(M.x, M.y, M.w, 1);
    b.fillStyle = C.stl2; b.fillRect(M.x, M.y + M.h - 3, M.w, 3);
    b.fillStyle = C.ink;  b.fillRect(M.x + 3, M.y + 3, M.w - 6, M.h - 8);
    b.fillStyle = C.on;   b.fillRect(M.x + M.w - 4, M.y + M.h - 2, 1, 1);   // power led
    b.fillStyle = C.stl2; b.fillRect(M.x + 12, M.y + M.h, 6, 3);            // neck
    b.fillStyle = C.stl4; b.fillRect(M.x + 8, M.y + M.h + 3, 14, 2);        // foot
    // keyboard on the desk in front of it
    b.fillStyle = C.stl2; b.fillRect(D.x + 16, D.y + 12, 26, 8);
    b.fillStyle = C.stl4; b.fillRect(D.x + 16, D.y + 12, 26, 1);
    b.fillStyle = C.stl1;
    for (let r = 0; r < 3; r++)
      for (let c = 0; c < 11; c++) b.fillRect(D.x + 18 + c * 2, D.y + 14 + r * 2, 1, 1);
    b.fillStyle = C.wood5;    b.fillRect(D.x + 50, D.y + 11, 8, 9);          // mug
    b.fillStyle = C.wood4;    b.fillRect(D.x + 51, D.y + 12, 6, 2);
    b.fillStyle = C.wood3;    b.fillRect(D.x + 58, D.y + 13, 2, 5);
    b.fillStyle = C.chairA;   b.fillRect(D.x + 20, D.y + 34, 22, 16);
    b.fillStyle = C.chairB;   b.fillRect(D.x + 22, D.y + 36, 18, 12);
    b.fillStyle = C.chairC;   b.fillRect(D.x + 22, D.y + 36, 18, 2);
    TERMINAL = {x: M.x + 3, y: M.y + 3, w: M.w - 6, h: M.h - 8};

    // armchair, seen from behind: back, arms, cushion
    b.fillStyle = C.rackEdge; b.fillRect(CHAIR.x - 2, CHAIR.y - 2, CHAIR.w + 4, CHAIR.h + 4);
    b.fillStyle = C.chairA;   b.fillRect(CHAIR.x, CHAIR.y, CHAIR.w, CHAIR.h);
    b.fillStyle = C.chairB;   b.fillRect(CHAIR.x + 4, CHAIR.y + 5, CHAIR.w - 8, CHAIR.h - 9);
    b.fillStyle = C.chairC;   b.fillRect(CHAIR.x + 4, CHAIR.y + 5, CHAIR.w - 8, 2);
    b.fillStyle = C.chairA;
    b.fillRect(CHAIR.x, CHAIR.y + 7, 5, CHAIR.h - 9);
    b.fillRect(CHAIR.x + CHAIR.w - 5, CHAIR.y + 7, 5, CHAIR.h - 9);
    b.fillStyle = C.cloth2;   b.fillRect(CHAIR.x + 6, CHAIR.y + CHAIR.h - 12, CHAIR.w - 12, 7);

    // shelf of books, with uneven spines
    b.fillStyle = C.rackEdge; b.fillRect(SHELF.x - 2, SHELF.y - 2, SHELF.w + 4, SHELF.h + 4);
    b.fillStyle = C.wood2;    b.fillRect(SHELF.x, SHELF.y, SHELF.w, SHELF.h);
    b.fillStyle = C.wood4;    b.fillRect(SHELF.x, SHELF.y, SHELF.w, 2);
    const spines = [C.book, C.bookB, C.bookC, C.book, C.bookB, C.bookC, C.book];
    for (let i = 0; i < spines.length; i++) {
      b.fillStyle = spines[i];
      const hgt = 4 + (i % 3);
      b.fillRect(SHELF.x + 2 + (i % 2), SHELF.y + 3 + i * 5, SHELF.w - 5 - (i % 3), hgt);
    }

    // a plant, because every lived-in room has one
    const PL = {x: 100, y: 252};
    b.fillStyle = C.wood2; b.fillRect(PL.x, PL.y, 15, 13);
    b.fillStyle = C.wood4; b.fillRect(PL.x, PL.y, 15, 2);
    b.fillStyle = C.onDim;
    for (const [dx, dy] of [[2,-8],[5,-13],[8,-9],[11,-6],[6,-6],[9,-14]])
      b.fillRect(PL.x + dx, PL.y + dy, 3, 3);
    b.fillStyle = C.on;
    for (const [dx, dy] of [[5,-13],[9,-14]]) b.fillRect(PL.x + dx, PL.y + dy, 2, 2);

    b.fillStyle = C.dim; b.font = "7px ui-monospace, monospace"; b.textAlign = "left";
    b.fillText("bunk", BUNK.x + 6, BUNK.y + BUNK.h - 7);
  }

  function lightPools(b) {
    // Soft overhead throw, not hard ellipses. The previous version banded badly:
    // a per-scanline ellipse with a squared falloff still ends on a visible
    // edge, and at this size that edge reads as a shape on the floor rather
    // than as light. This uses a real radial gradient and a dithered outer
    // fringe so the last few percent dissolves into the floor texture.
    const pool = (cx, cy, rx, ry, peak) => {
      b.save();
      b.translate(cx, cy);
      b.scale(1, ry / rx);
      const grad = b.createRadialGradient(0, 0, 0, 0, 0, rx);
      grad.addColorStop(0.00, "rgba(168,188,230," + peak + ")");
      grad.addColorStop(0.45, "rgba(168,188,230," + (peak * 0.45).toFixed(3) + ")");
      grad.addColorStop(0.78, "rgba(168,188,230," + (peak * 0.12).toFixed(3) + ")");
      grad.addColorStop(1.00, "rgba(168,188,230,0)");
      b.fillStyle = grad;
      b.beginPath(); b.arc(0, 0, rx, 0, Math.PI * 2); b.fill();
      b.restore();
      // dither the fringe so any residual banding breaks up on the pixel grid
      b.fillStyle = "rgba(168,188,230,0.028)";
      for (let i = 0; i < 40; i++) {
        const t = 0.88 + hash2(cx + i, cy) * 0.10;
        const a = (i / 40) * Math.PI * 2;
        const px = Math.round(cx + Math.cos(a) * rx * t);
        const py = Math.round(cy + Math.sin(a) * ry * t);
        if ((px + py) % 2 === 0) b.fillRect(px, py, 1, 1);
      }
    };
    // one per ceiling fitting, so the pools line up with the aisles
    pool(264, AISLE_MAIN - 8, 96, 26, 0.10);
    pool(393, AISLE_MAIN - 8, 84, 24, 0.09);
    pool(309, AISLE_FRONT - 4, 112, 26, 0.09);
    pool(BUNK.x + 56, BUNK.y + 132, 62, 22, 0.08);
  }

  function drawRack(r) {
    const top = r.base - RACK_H, x = r.x;
    // outline in a darker version of the steel, never black -- black outlines
    // punch holes in a dark room
    ctx.fillStyle = C.rackEdge; ctx.fillRect(x - 2, top - 2, RACK_W + 4, RACK_H + 4);

    // top face, lit from upper-left
    ctx.fillStyle = C.stl4; ctx.fillRect(x, top, RACK_W, TOP_H);
    ctx.fillStyle = C.stl6; ctx.fillRect(x, top, RACK_W, 1);
    ctx.fillStyle = C.stl5; ctx.fillRect(x, top, 2, TOP_H);
    ctx.fillStyle = C.stl2; ctx.fillRect(x + RACK_W - 2, top + 1, 2, TOP_H - 1);

    // front face + mounting rails down each side
    ctx.fillStyle = C.stl3; ctx.fillRect(x, top + TOP_H, RACK_W, RACK_H - TOP_H);
    ctx.fillStyle = C.stl4; ctx.fillRect(x, top + TOP_H, 3, RACK_H - TOP_H);
    ctx.fillStyle = C.stl2; ctx.fillRect(x + RACK_W - 3, top + TOP_H, 3, RACK_H - TOP_H);
    ctx.fillStyle = C.rail;
    for (let i = 0; i < 6; i++) {
      ctx.fillRect(x + 1, top + TOP_H + 3 + i * 6, 1, 2);
      ctx.fillRect(x + RACK_W - 2, top + TOP_H + 3 + i * 6, 1, 2);
    }

    // 1U units: bezel, vent mesh, a handle, and a drive bay
    for (let u = 0; u < 5; u++) {
      const uy = top + TOP_H + 10 + u * 6;
      const g = r.guests[u];
      ctx.fillStyle = C.stl2; ctx.fillRect(x + 3, uy, RACK_W - 6, 5);
      ctx.fillStyle = C.stl4; ctx.fillRect(x + 3, uy, RACK_W - 6, 1);
      ctx.fillStyle = C.vent;
      for (let v = 0; v < 7; v++) ctx.fillRect(x + 6 + v * 2, uy + 2, 1, 2);
      ctx.fillStyle = C.handle; ctx.fillRect(x + 4, uy + 2, 1, 2);
      if (g) { ctx.fillStyle = C.stl5; ctx.fillRect(x + RACK_W - 8, uy + 1, 3, 3); }
    }

    // plinth and a contact shadow on the floor
    ctx.fillStyle = C.stl1; ctx.fillRect(x, r.base - 3, RACK_W, 3);
    ctx.fillStyle = "rgba(11,10,16,.42)";
    ctx.fillRect(x - 3, r.base, RACK_W + 6, 3);

    // Label plate on the rack itself. Floating text above an object reads as UI
    // hovering in the air; screwed to the chassis it reads as part of the room.
    const plateY = top + TOP_H + 1;
    ctx.fillStyle = C.stl1; ctx.fillRect(x + 3, plateY, RACK_W - 6, 7);
    ctx.fillStyle = C.stl2; ctx.fillRect(x + 3, plateY, RACK_W - 6, 1);
    ctx.fillStyle = burning.has(r.node) ? C.fMid : C.stl6;
    ctx.font = "6px ui-monospace, monospace"; ctx.textAlign = "center";
    ctx.fillText(r.node.replace("prxy-", ""), x + RACK_W / 2, plateY + 6);
    ctx.textAlign = "left";
    if (burning.has(r.node)) {
      const seed = r.x * 31 + r.base;
      const wet = fightingHere(r) ? doused(nowMs, seed) : 1;
      drawFire(x, top, RACK_W, nowMs, seed, wet);
      pendingGlow.push([x, top - 9, RACK_W, seed, wet]);
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
    // digits are 3 wide at 2x with 2px gaps, plus a 4px colon: measure it and
    // centre the block in the display rather than guessing an offset
    const DW = 6, GAP = 2, COLON = 5;
    const total = DW * 4 + GAP * 3 + COLON;
    const x = CLOCK.x + Math.round((CLOCK.w - total) / 2);
    const y = CLOCK.y + Math.round((CLOCK.h - 10) / 2);
    const cols = [x, x + DW + GAP, x + (DW + GAP) * 2 + COLON, x + (DW + GAP) * 3 + COLON];
    const big = (map, ch, gx, colour) => {
      const g = map[ch]; if (!g) return;
      ctx.fillStyle = colour;
      for (let r = 0; r < 5; r++)
        for (let c = 0; c < 3; c++)
          if (g[r][c] === "#") ctx.fillRect(gx + c * 2, y + r * 2, 2, 2);
    };
    for (const gx of cols) big(DIGIT, "8", gx, C.ledDim);   // unlit segments
    big(DIGIT, hh[0], cols[0], C.ledOn); big(DIGIT, hh[1], cols[1], C.ledOn);
    big(DIGIT, mm[0], cols[2], C.ledOn); big(DIGIT, mm[1], cols[3], C.ledOn);
    const cx = x + (DW + GAP) * 2 + 1;
    ctx.fillStyle = d.getSeconds() % 2 ? C.ledDim : C.ledOn;
    ctx.fillRect(cx, y + 2, 2, 2); ctx.fillRect(cx, y + 6, 2, 2);
    ctx.globalCompositeOperation = "lighter";
    ctx.globalAlpha = 0.07; ctx.fillStyle = C.ledOn;
    ctx.fillRect(CLOCK.x + 4, CLOCK.y + 4, CLOCK.w - 8, CLOCK.h - 8);
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
    x: BED.x + 21, y: BED.y + 40, dir: "down", flip: false,
    dist: 0, asleep: true, activity: "sleep", say: "", sayUntil: 0,
    lastEvent: Date.now(),
  };
  const DESK_SEAT = {x: DESK.x + 31, y: DESK.y + 54};   // in front of the desk chair
  const COOK_SPOT = {x: KITCHEN.x + 22, y: KITCHEN.y + KITCHEN.h + 22};
  const READ_SPOT = {x: CHAIR.x + 17, y: CHAIR.y + CHAIR.h + 6};
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
    walk(BED.x + 21, BED.y + 40, null);
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
    sleep: () => ({x: BED.x + 21, y: BED.y + 40}),
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
    items.push({sortY: fred.asleep ? BED.y + 15 : fred.y, sortX: fred.x, fred: true});
    items.sort((a, b) => (a.sortY - b.sortY) || (a.sortX - b.sortX));

    for (const it of items) {
      if (!it.fred) { drawRack(it.r); continue; }
      if (fred.asleep) {
        const rise = Math.floor(now / 1000) % 2 ? -1 : 0;
        blit(SLEEP, BED.x + 5, BED.y + 15 + rise, false);
        continue;
      }
      ctx.fillStyle = "rgba(11,10,16,.45)";
      ctx.fillRect(Math.round(fred.x) - 7, Math.round(fred.y) - 2, 14, 3);

      // Standing still is the least informative thing he can do, so every
      // arrival hands off to a pose that says what he is actually doing.
      if (!walking && fred.activity === "read") {
        blit(SPR.side.slice(0, 22), fred.x - 9, fred.y - 22, fred.flip);
        drawBook(fred.x - 9, fred.y, now);
      } else if (!walking && fred.activity === "cook") {
        const stir = Math.floor(now / 300) % 2;
        blit(SPR.up, fred.x - 9, fred.y - 27 - stir, false);
      } else if (!walking && fred.activity === "type") {
        blit(SPR.up.slice(0, 20), fred.x - 9, fred.y - 20, false);
      } else if (!walking && fred.activity === "inspect") {
        blit(SPR.up.slice(0, 24), fred.x - 9, fred.y - 24 + (Math.floor(now / 260) % 2), false);
      } else if (!walking && fred.activity === "fight") {
        // Facing the fire with a bucket. He braces on the throw, which is the
        // only frame that reads as effort at 12px wide.
        const target = racks.find(r => burning.has(r.node) && fightingHere(r));
        const seed = target ? target.x * 31 + target.base : 0;
        const p = throwPhase(now, seed);
        const brace = (p >= RELEASE && p < RELEASE + 0.14) ? 1 : 0;
        blit(SPR.up, fred.x - 9, fred.y - 27 + brace, false);
        drawBucket(fred.x - 9, fred.y, now, seed);
        if (target) {
          drawWater(fred.x - 9, fred.y,
                    target.x + RACK_W / 2, target.base - RACK_H + 2, now, seed);
        }
      } else {
        const key = (fred.dir === "left" || fred.dir === "right") ? "side" : fred.dir;
        const rows = apart ? SPR[key].slice(0, 21).concat(LEGS_APART[key]) : SPR[key];
        blit(rows, fred.x - 9, fred.y - 27 + (apart ? -1 : 0), fred.flip);
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
