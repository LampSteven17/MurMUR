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
    drape:"#5c3346", drapeLit:"#7d4a60", drapeDark:"#3a1f2e", rod:"#6b5a48",
    tvGlow:"#2c4a5e",
    neonHot:"#ff5cc8", neonCore:"#ffd6f2", neonDim:"#8a3a6e", neonDead:"#4d3d59",
    tube:"#241c2c", cable:"#3d3550", cableLit:"#544a68",
    ledOn:"#f2555a", ledDim:"#4a1d22", bezel:"#16131f", bezelTop:"#2c2636",
    // Fred: warm against a cool room, with enough steps to shade at 18px wide
    hair1:"#2b1c12", hair2:"#4a3526", hair3:"#6b4d36",
    // Ginger afro, three shades hue-shifting toward red as they darken, plus a
    // silhouette shade darker than any of them. Stardew outlines its characters
    // where Kindergarten deliberately does not.
    afro1:"#5a2110", afro2:"#b8431f", afro3:"#e0763a", line:"#241812",
    iris:"#3f6b4a",
    skin1:"#b57a52", skin2:"#e0a578", skin3:"#f2c9a0", eyeLit:"#fdf3e6",
    // Kindergarten's grammar: the sclera is a warm cream, never white, the
    // pupil is a blue-tinted near-black rather than pure #000, and the mouth is
    // the darkest skin shade -- a ~57% value drop from the light tone, not the
    // 24% mine had, which is why it read as a smudge instead of a mouth.
    skin0:"#6b4a33", eyeW:"#faf1e0", eyeP:"#2f2722", blush:"#de8098",
    shirt1:"#5e3a20", shirt2:"#8a5730", shirt3:"#b0764a",
    pant1:"#232840", pant2:"#343c5c", boot:"#15121f",
    catA:"#3a2214", catB:"#a86534", catC:"#d9995a", catEye:"#9be36a",
    ballA:"#d94f6a", ballB:"#f2899b", mess:"#6d5a3f",
    txt:"#c9d4e8", dim:"#5a6488", rug:"#2a2f47", rugLit:"#39405e",
    quilt:"#6e4450", pillow:"#a9bad9", bedFrame:"#4a3524", bedTop:"#6b4c33",
    rackEdge:"#0d0c14", rackTop:"#646d92", rackFace:"#343a55", rackLit:"#4a5273",
    wallCap:"#4a4d70", wall:"#242439", floorA:"#262940", floorB:"#2f334e",
    dark:"#0d0c14",
  };
  // --- character sprites: 12 wide, 18 tall, one char per pixel -------------
  // Two leg positions per direction. The stride is carried by swapping the legs
  // AND lifting the whole sprite 1px — animating legs alone reads as shuffling.
  const SPR = {
    // 18x27, moved off the Kindergarten grammar toward Stardew's.
    //
    // Head plus hair is 44% of his height now rather than 56% -- Stardew world
    // sprites run nearer a third, Kindergarten well over half, and this sits
    // between them so he still reads as the same character. The body got the
    // rows back, so he has a torso again instead of a head on legs.
    //
    // What is Stardew and not Kindergarten: a dark silhouette line (Kindergarten
    // has none at all), a lash line above each eye, and an iris colour distinct
    // from the pupil. What is kept from before, because it is what stopped him
    // looking dead: the eye is still mostly sclera with the dark part small, and
    // both pupils sit on the same side rather than mirrored.
    //
    // The afro is three shades plus the silhouette, hue-shifting toward red as
    // it darkens, and it reads as the widest part of him -- which is the point.
    down: [
      "....FFFFFFFFFF....","..FFAAAAAAAAAAFF..",".FFAGGGAAAAAAAAFF.",".FAGGGAAAAAAAAAAF.",
      ".FAASSSSSSSSSSAAF.",".FAASSSSSSSSSSAAF.",".FAAkkkSSSSkkkAAF.",".FAAWIWSSSSWIWAAF.",
      ".FAAWEWSSSSWEWAAF.",".FAArSSSSSSSSrAAF.",".FAASSSmmmmSSSAAF.","..FASSSSSSSSSSAF..",
      "......SSSSSS......","...CCCCCCCCCCCC...","..ccCCCCCCCCCCcc..","..cLCCCCCCCCCCLc..",
      "..cLCCCCCCCCCCLc..","..cLCCCCCCCCCCLc..","..cLCCCCCCCCCCLc..","..ssCCCCCCCCCCss..",
      "..ssCCCCCCCCCCss..","...PPPPPPPPPPPP...","...PPPPPPPPPPPP...","...PPPPP..PPPPP...",
      "...PPPPP..PPPPP...","...bbbb....bbbb...","...bbbb....bbbb...",
    ],
    up: [
      "....FFFFFFFFFF....","..FFAAAAAAAAAAFF..",".FFAGGGAAAAAAAAFF.",".FAGGGAAAAAAAAAAF.",
      ".FAGGAAAAAAAAAAAF.",".FAAAAAAAAAAAAAAF.",".FAAAAAAAAAAAAAAF.",".FAAAAAAAAAAAAAAF.",
      ".FAAAAAAAAAAAAAAF.",".FFAAAAAAAAAAAAFF.",".FFFAAAAAAAAAAFFF.","..FFFFFFFFFFFFFF..",
      "......SSSSSS......","...CCCCCCCCCCCC...","..ccCCCCCCCCCCcc..","..cLCCCCCCCCCCLc..",
      "..cLCCCCCCCCCCLc..","..cLCCCCCCCCCCLc..","..cLCCCCCCCCCCLc..","..ssCCCCCCCCCCss..",
      "..ssCCCCCCCCCCss..","...PPPPPPPPPPPP...","...PPPPPPPPPPPP...","...PPPPP..PPPPP...",
      "...PPPPP..PPPPP...","...bbbb....bbbb...","...bbbb....bbbb...",
    ],
    stretch: [
      "....FFFFFFFFFF....","..FFAAAAAAAAAAFF..",".FFAAGGAAAAAAAAFF.",".FAAAGGAAAAAAAAAF.",
      ".FAAAAAAAAAAAAAAF.",".FAAAAAAAAAAAAAAF.",".FAAAAAAAAAAAAAAF.",".FAAAAAAAAAAAAAAF.",
      ".FAAAAAAAAAAAAAAF.","sFAAAAAAAAAAAAAAFs","sFAAAAAAAAAAAAAAFs","sSFAAAAAAAAAAAAFSs",
      "sS....SSSSSS....Ss","cC.CCCCCCCCCCCC.Cc","..cCCCCCCCCCCCCc..","...CCCCCCCCCCCC...",
      "...CCCCCCCCCCCC...","...CCCCCCCCCCCC...","...CCCCCCCCCCCC...","...CCCCCCCCCCCC...",
      "...CCCCCCCCCCCC...","...PPPPPPPPPPPP...","...PPPPPPPPPPPP...","...PPPPP..PPPPP...",
      "...PPPPP..PPPPP...","...bbbb....bbbb...","...bbbb....bbbb...",
    ],
    // Profile facing right: the afro sits behind the head, the eye and mouth
    // forward of centre.
    side: [
      "...FFFFFFFF.......",".FFAAAAAAAAF......",".FAGGGAAAAAAAF....",".FAGGAAAAAAAAF....",
      ".FAAAAASSSSSSSF...",".FAAAAASSSSSSSF...",".FAAAAASSSkkkSF...",".FAAAAASSSWIWSF...",
      ".FAAAAASSSWEWSF...",".FAAAArSSSSSSSF...",".FAAAAASSSmmSSF...","..FAAAASSSSSSF....",
      "......SSSSS.......","...CCCCCCCCCC.....","..cCCCCCCCCCCc....","..cLCCCCCCCCLc....",
      "..cLCCCCCCCCLc....","..cLCCCCCCCCLc....","..cLCCCCCCCCLc....","..ssCCCCCCCCss....",
      "..ssCCCCCCCCss....","...PPPPPPPPPP.....","...PPPP..PPPP.....","...PPPP..PPPP.....",
      "...bbb....bbb.....","...bbb....bbb.....","..................",
    ],
    sideStretch: [
      "...FFFFFFFF.......",".FFAAAAAAAAF......",".FAGGGAAAAAAAF....",".FAGGAAAAAAAAF....",
      ".FAAAAASSSSSSSF...",".FAAAAASSSSSSSF...",".FAAAAASSSkkkSF...",".FAAAAASSSWIWSF...",
      ".FAAAAASSSWEWSF...",".FAAAArSSSSSSSF...",".FAAAAASSSmmSSF...","..FAAAASSSSSSF.ss.",
      "......SSSSS....ss.","...CCCCCCCCCC..ss.","..cCCCCCCCCCCc.ss.","..cCCCCCCCCCCc....",
      "..cCCCCCCCCCCc....","..cCCCCCCCCCCc....","..cCCCCCCCCCCc....","..cCCCCCCCCCCc....",
      "..cCCCCCCCCCCc....","...PPPPPPPPPP.....","...PPPP..PPPP.....","...PPPP..PPPP.....",
      "...bbb....bbb.....","...bbb....bbb.....","..................",
    ],
  };

  // Eye rows only, swapped in for a blink. The lid is the dark skin tone, not
  // ink -- a black line across the eyes is a hole in the face. The lash line on
  // row 6 stays put, which is what keeps the eye readable while it is shut.
  const BLINK_ROWS = {
    down: [".FAASSSSSSSSSSAAF.", ".FAAmmmSSSSmmmAAF."],
    side: [".FAAAAASSSSSSSF...", ".FAAAAASSSmmSSF..."],
  };
  const blinking = now => (now + 900) % 4300 < 130;
  function withBlink(rows, key, now) {
    const b = BLINK_ROWS[key];
    if (!b || !blinking(now)) return rows;
    const out = rows.slice();
    out[7] = b[0]; out[8] = b[1];
    return out;
  }

  const HANDS = {
    // The front/back sprites and the profile have their arms in different
    // columns, so the swap set is per-view. Getting this wrong corrupts the
    // torso rather than moving an arm.
    wide: {
      rest: ["..cLCCCCCCCCCCLc..", "..ssCCCCCCCCCCss..", "..ssCCCCCCCCCCss.."],
      a:    ["..ssCCCCCCCCCCLc..", "..ssCCCCCCCCCCss..", "..cLCCCCCCCCCCss.."],
      b:    ["..cLCCCCCCCCCCss..", "..ssCCCCCCCCCCss..", "..ssCCCCCCCCCCLc.."],
    },   // rows 18-20
    side: {
      rest: ["..cLCCCCCCCCLc....", "..ssCCCCCCCCss....", "..ssCCCCCCCCss...."],
      a:    ["..ssCCCCCCCCLc....", "..ssCCCCCCCCss....", "..cLCCCCCCCCss...."],
      b:    ["..cLCCCCCCCCss....", "..ssCCCCCCCCss....", "..ssCCCCCCCCLc...."],
    },
  };
  function withHands(rows, which, key) {
    const set = HANDS[key === "side" ? "side" : "wide"];
    const h = set && set[which];
    if (!h) return rows;
    const out = rows.slice();
    out[18] = h[0]; out[19] = h[1]; out[20] = h[2];
    return out;
  }

  // Rows 21-26, swapped in on alternate strides.
  const LEGS_APART = {
    down: ["...PPPPPPPPPPPP...","..PPPPP.....PPPP..",".PPPP.........PPP.",
           ".bbbb.........bbb.",".bbbb.........bbb.","..................",],
    up:   ["...PPPPPPPPPPPP...","..PPPPP.....PPPP..",".PPPP.........PPP.",
           ".bbbb.........bbb.",".bbbb.........bbb.","..................",],
    side: ["...PPPPPPPPPP.....","..PPPPP..PPPP.....",".PPPP.....PPP.....",
           ".bbbb.....bbb.....",".bbbb.....bbb.....","..................",],
  };

  const SLEEP = [
    "........hhhhhh................","......hhhhhhhhhh..............",
    ".....thhSSSSSShH..............",".....thSSSSSSSSH..............",
    ".....thSmmSSmmSH..............",".....thSSSSSSSSH..............",
    "......hSSSSSSH................","......QQQQQQQQQQQQQQQQQQQQ....",
    ".....QQQQQQQQQQQQQQQQQQQQQQ...",".....QQQQQQQQQQQQQQQQQQQQQQ...",
    ".....qQQQQQQQQQQQQQQQQQQQQq...",".....qqQQQQQQQQQQQQQQQQQQqq...",
    "......qqqqqqqqqqqqqqqqqqqq....","..............................",
  ];

  const PX = {
    h:C.hair2, H:C.hair1, t:C.hair3,
    A:C.afro2, F:C.afro1, G:C.afro3, k:C.line, I:C.iris,
    s:C.skin2, S:C.skin3, d:C.skin1, E:C.eyeP, w:C.eyeLit,
    W:C.eyeW, m:C.skin0, r:C.blush,
    C:C.shirt2, L:C.shirt3, c:C.shirt1,
    P:C.pant2, p:C.pant1, b:C.boot,
    Q:C.quilt, q:C.cloth1, "-":C.ink,
    a:C.catA, B:C.catB, e:C.catC, y:C.catEye,
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


  // --- solid geometry ---------------------------------------------------------
  // Everything was a facade because the projection made it one. At yaw 0 the
  // depth and height axes are collinear on screen, so a side face has exactly
  // zero width -- no shading can recover a face with no pixels. Shearing the
  // depth axis gives it area back.
  //
  // Cabinet oblique on a 2:1 pixel lattice:
  //   depth rises 1px up-screen per 2 of depth, and leans 1px sideways per 4.
  //   That is a 2:1 staircase (run lengths 2,2,2,... -- the cleanest diagonal),
  //   a 63.43deg depth axis, and a 0.56 depth scale, which is textbook cabinet.
  //   Height stays at scale 1.0, so every front face already drawn still fits.
  const DEPTH_RISE = 0.5, DEPTH_SHEAR = 0.25, LEAN = 1;   // +1: right side shows

  // Light is upper-left, so the visible (right) side is the shadow side and the
  // faces form a monotone ladder: edge > top > front > side.
  function boxCorners(ox, oy, w, d, h) {
    // Rounded ONCE. Rounding per corner lets the floor and top quads disagree
    // by a pixel, and the box visibly warps.
    const dy = Math.round(d * DEPTH_RISE);
    const dx = Math.round(d * DEPTH_SHEAR) * LEAN;
    return {dx, dy, ox, oy, w, h};
  }

  function fillTopFace(g, c, colour) {
    g.fillStyle = colour;
    for (let i = 0; i < c.dy; i++) {
      const y = c.oy - c.h - 1 - i;
      const x = c.ox + Math.round(c.dx * (i + 1) / c.dy);
      g.fillRect(x, y, c.w, 1);       // rows, not a path: ctx.fill() antialiases
    }
  }

  // A sub-rectangle of a box's top face, in the face's own (u across, v back)
  // coordinates. Bedding, counter clutter and rugs have to ride the same shear
  // as the face they sit on, or they read as floating above the object.
  function fillTopBand(g, c, colour, u0, u1, v0, v1) {
    g.fillStyle = colour;
    const i0 = Math.round(v0 * c.dy), i1 = Math.round(v1 * c.dy);
    const bx = Math.round(u0 * c.w), bw = Math.max(1, Math.round((u1 - u0) * c.w));
    for (let i = i0; i < i1; i++) {
      const y = c.oy - c.h - 1 - i;
      const x = c.ox + Math.round(c.dx * (i + 1) / c.dy) + bx;
      g.fillRect(x, y, bw, 1);
    }
  }

  function fillSideFace(g, c, colour) {
    if (!c.dx) return;
    g.fillStyle = colour;
    const n = Math.abs(c.dx), step = Math.sign(c.dx);
    for (let j = 1; j <= n; j++) {
      const x = c.ox + c.w + (step > 0 ? j - 1 : -j);
      const floorY = c.oy - Math.round(c.dy * j / n);
      g.fillRect(x, floorY - c.h, 1, c.h);   // columns
    }
  }

  // mat = {top, front, side, edge, dark} taken straight from a palette ramp
  function drawBox(g, ox, oy, w, d, h, mat, opts) {
    opts = opts || {};
    const c = boxCorners(ox, oy, w, d, h);

    if (opts.shadow !== false) {
      // Footprint quad, offset away from the light. Fixed length, NOT scaled by
      // height: a tall rack would otherwise smear a stripe across the floor.
      const so = h > 32 ? 6 : 4, sv = h > 32 ? 3 : 2;
      g.save(); g.globalAlpha = 0.30; g.fillStyle = "#000";
      for (let i = 0; i < c.dy; i++) {
        const y = c.oy - 1 - i;
        const x = c.ox + Math.round(c.dx * (i + 1) / c.dy);
        g.fillRect(x + so, y + sv, w, 1);
      }
      g.globalAlpha = 0.16;                       // tight symmetric AO, no offset
      g.fillRect(c.ox - 1, c.oy, w + 2, 2);
      g.restore();
    }

    // Silhouette underlay. Without it the faces sit at nearly the same value as
    // the floor and the whole box dissolves -- the old flat racks were readable
    // only because they carried a hard dark rectangle behind them.
    //
    // opts.bare skips it, and the contact and corner lines below. A sub-box of
    // a composite (a chair arm against its seat) must not carry its own
    // outline: two abutting boxes stack their dark edges into a fat black seam
    // and the object falls apart into slabs. Only the outermost box outlines.
    if (!opts.bare) {
      const o = {dx: c.dx, dy: c.dy, ox: c.ox - 1, oy: c.oy + 1, w: w + 2, h: h + 1};
      g.fillStyle = mat.dark;
      fillSideFace(g, o, mat.dark);
      g.fillRect(o.ox, o.oy - o.h - 1, o.w, o.h + 1);
      fillTopFace(g, o, mat.dark);
    }

    fillSideFace(g, c, mat.side);
    g.fillStyle = mat.front; g.fillRect(c.ox, c.oy - h, w, h);
    fillTopFace(g, c, mat.top);

    if (h > 6) {                                  // occlusion at the base
      g.fillStyle = mat.side;
      g.fillRect(c.ox, c.oy - Math.min(3, h), w, Math.min(3, h));
    }

    // The 1px edges carry more form than the fills do. The top-front edge is
    // the single most valuable line: it is what says a horizontal plane ends here.
    g.fillStyle = mat.edge;
    g.fillRect(c.ox, c.oy - h - 1, w, 1);
    for (let i = 0; i < c.dy; i++) {
      const y = c.oy - h - 1 - i;
      g.fillRect(c.ox + Math.round(c.dx * (i + 1) / c.dy), y, 1, 1);
    }
    if (!opts.bare) {
      g.fillStyle = mat.dark;
      g.fillRect(c.ox, c.oy - 1, w, 1);           // floor contact
      g.fillRect(c.ox + w - 1, c.oy - h, 1, h);   // near vertical corner
    }
    return c;
  }

  // Materials, built from the ramps already in the palette rather than computed:
  // the ramps are hue-shifted by hand, which is what keeps a dark object and a
  // pale one showing the same apparent step contrast.
  const MAT = {
    steel: {top:C.stl4, front:C.stl3, side:C.stl2, edge:C.stl6, dark:C.stl1},
    wood:  {top:C.wood4, front:C.wood3, side:C.wood2, edge:C.wood5, dark:C.wood1},
    unit:  {top:C.counterTop, front:C.counter, side:C.chairA, edge:C.counterLit, dark:C.ink},
    chair: {top:C.chairC, front:C.chairB, side:C.chairA, edge:C.cloth3, dark:C.ink},
    cloth: {top:C.cloth3, front:C.cloth2, side:C.cloth1, edge:C.cloth4, dark:C.ink},
  };

  // --- room geometry -------------------------------------------------------
  const WALL_H = 70, FLOOR_B = 288;
  const BUNK = {x:9, y:72, w:154, h:210};
  // Studio interior is x 9..163, y 76..282. Everything below is placed against a
  // wall with a clear gap to its neighbours -- the previous pass had the shelf
  // sitting inside the desk.
  // Every piece is anchored to a wall by construction, not by eye. A box's back
  // edge sits at oy - h - dy, so an object flush against the back wall has its
  // base at WALL_Y + h + dy -- backBase() computes that instead of me guessing a
  // number and finding the gap later. Each piece keeps {x,y,w,h} as its visual
  // footprint (y is the back edge, y+h the base) because Fred's walk targets are
  // expressed against those.
  // Everything is scaled off Fred. He is 27px tall, which reads as about 1.7m,
  // so heights run at roughly 16px per metre: a fridge is a shade taller than
  // he is, a counter hits him at the waist, a desk mid-thigh. Small objects
  // (the telly) stay deliberately over-scale, because a truly-to-scale 24"
  // screen is 6px and unreadable.
  //
  // WALL_Y is the line where the floor meets the back wall, and a piece stands
  // against that wall when its FLOOR FOOTPRINT touches it -- base - dy -- not
  // when its top face does. The previous form added the piece's height, which
  // anchored the top face instead and left the base that many pixels out into
  // the room: the fridge had its lid in the corner and its body a foot away.
  // A tall piece now correctly rises UP the wall in view, which is what a tall
  // thing standing against a wall does.
  const WALL_Y = WALL_H + 2;
  const WINDOW = {x:43, y:10, w:88, h:44};
  const backBase = (h, d) => WALL_Y + Math.round(d * DEPTH_RISE);
  const piece = (x, w, d, h, base) =>
    ({x, w, d, bh: h, y: base - h - Math.round(d * DEPTH_RISE),
      h: h + Math.round(d * DEPTH_RISE)});

  // A point on a piece's TOP face, in that face's own (u across, v back)
  // coordinates. Anything that sits ON a surface -- a pot, a mug, a seated
  // character -- must be placed through this, because the face is sheared and
  // a screen-space offset from the piece's origin lands on the FRONT face
  // instead. That mistake produced the floating hobs, the keyboard hanging off
  // the desk, and a saucepan on a cupboard door.
  const topPoint = (P, u, v) => {
    const dy = Math.round(P.d * DEPTH_RISE), dx = Math.round(P.d * DEPTH_SHEAR);
    const i = Math.max(0, Math.min(dy - 1, Math.round(v * (dy - 1))));
    return {
      x: P.x + Math.round(dx * (i + 1) / dy) + Math.round(u * P.w),
      y: (P.y + P.h) - P.bh - 1 - i,
    };
  };
  const FRIDGE  = piece(10,  20, 14, 30, backBase(30, 14));   // hard into the corner
  const KITCHEN = piece(59,  56, 18, 15, backBase(15, 18));   // centred under the window
  const SHELF   = piece(140, 20, 10, 30, backBase(30, 10));   // flush to the side wall
  // The bed's long axis runs left-right because that is how Fred lies on it;
  // its top face has to be wide enough for a 30px sprite and deep enough for 14
  // rows of one, which is what sets w and d rather than any real bed's ratio.
  const BED     = piece(12,  48, 30,  8, 264);   // longer, moved to the front
  const TVST    = piece(12,  36, 16, 10, 190);   // against the left wall
  const TV      = piece(20,  24,  8, 16, TVST.y + TVST.h - TVST.bh - 3);
  // One seat and one screen. The desk PC and the armchair are gone -- he sits on
  // this stool in front of the telly, and the telly is his terminal.
  const STOOL   = piece(70,  22, 12, 10, 196);   // to the right of the telly
  const PLANT   = piece(142, 14, 10, 11, 268);
  const DOOR = {y0:238, y1:280};
  const RACK_W = 28, RACK_H = 48, TOP_H = 9, RACK_D = 16;
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
    // Racks are solid, so the collision world and both nav grids can only be
    // built once we know where they are.
    buildSolids();
    buildGrid("fred", FRED_HW, FRED_FH);
    buildGrid("cat", CAT_HW, CAT_FH);
  }

  function rebuildLeds() {
    leds.length = 0;
    // Non-harmonic periods: with shared factors the whole room visibly blinks in
    // unison within seconds and reads as one machine instead of eight.
    const PERIODS = [370, 530, 790, 1130, 1490];
    racks.forEach((r, ri) => {
      const top = r.base - RACK_H + 9;
      for (let s = 0; s < 6; s++) {
        const g = r.guests[s];
        leds.push({
          x: r.x + RACK_W - 8, y: top + 5 + s * 6,
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
    // No backing panel. A neon sign is glass tubes on standoffs -- a filled box
    // behind them is what made this read as a lightbox with letters printed on
    // it. What is left is the hardware that would actually be there: a mounting
    // rail, the standoffs, a transformer, and the conduit between them.
    b.fillStyle = C.stl2; b.fillRect(SIGN.x - 4, SIGN.y - 7, SIGN.w + 8, 2);
    b.fillStyle = C.stl4; b.fillRect(SIGN.x - 4, SIGN.y - 7, SIGN.w + 8, 1);
    b.fillStyle = C.stl1;
    for (const sx of [SIGN.x - 2, SIGN.x + SIGN.w]) b.fillRect(sx, SIGN.y - 5, 2, 6);

    // Transformer box, hung off the left end, with its own tired little LED.
    const TB = {x: SIGN.x - 14, y: SIGN.y + 2, w: 11, h: 13};
    b.fillStyle = C.stl1; b.fillRect(TB.x, TB.y, TB.w, TB.h);
    b.fillStyle = C.stl3; b.fillRect(TB.x, TB.y, TB.w, 1);
    b.fillStyle = C.ink;  b.fillRect(TB.x + 2, TB.y + 3, TB.w - 4, 5);
    b.fillStyle = C.cable;
    b.fillRect(TB.x + TB.w, TB.y + 4, 5, 1);          // conduit into the tubes
    b.fillRect(TB.x + 4, TB.y - 7, 1, 7);             // feed up to the rail

    // Wires hanging off the bottom rail: 1px, because a 3px cable at this size
    // reads as a pipe. One of them is cut.
    const droop = (x0, x1, sag, cut) => {
      const span = x1 - x0;
      for (let i = 0; i <= span; i++) {
        const t = i / span;
        const yy = SIGN.y + SIGN.h + 1 + Math.round(Math.sin(t * Math.PI) * sag);
        b.fillStyle = C.cable; b.fillRect(x0 + i, yy, 1, 2);
        if (i % 11 === 5) { b.fillStyle = C.stl1; b.fillRect(x0 + i, yy - 1, 1, 4); }  // tape
      }
      if (cut) {
        const yy = SIGN.y + SIGN.h + 1;
        b.fillStyle = C.cable;
        for (let k = 1; k <= 9; k++) b.fillRect(x1 + (k > 5 ? 1 : 0), yy + k, 1, 1);
        b.fillStyle = C.bad; b.fillRect(x1 + 1, yy + 10, 2, 2);   // bare copper
      }
    };
    droop(SIGN.x + 8, SIGN.x + 38, 9, false);
    droop(SIGN.x + 46, SIGN.x + 71, 6, true);

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
    b.fillStyle = C.rug; b.fillRect(BUNK.x, BUNK.y, BUNK.w, BUNK.h);
    b.fillStyle = C.rugLit; b.fillRect(BUNK.x + 3, BUNK.y + 3, BUNK.w - 6, 2);
    for (let y = BUNK.y + 10; y < BUNK.y + BUNK.h - 4; y += 9) {
      b.fillStyle = C.rugLit; b.fillRect(BUNK.x + 6, y, BUNK.w - 12, 1);
    }
    b.fillStyle = C.wall1;
    b.fillRect(BUNK.x + BUNK.w, BUNK.y - 6, 4, DOOR.y0 - BUNK.y + 6);
    b.fillRect(BUNK.x + BUNK.w, DOOR.y1, 4, FLOOR_B - DOOR.y1);
    b.fillRect(BUNK.x - 4, BUNK.y - 6, 4, BUNK.h + 6);
    b.fillStyle = C.wall4; b.fillRect(BUNK.x + BUNK.w, BUNK.y - 6, 4, 2);

    const box = (P, mat, opts) => drawBox(b, P.x, P.y + P.h, P.w, P.d, P.bh, mat, opts);
    const faceTop = (P) => P.y + P.h - P.bh;      // top of a piece's FRONT face

    // --- back wall ---------------------------------------------------------
    box(FRIDGE, MAT.steel);
    const fT = faceTop(FRIDGE);
    b.fillStyle = C.stl1; b.fillRect(FRIDGE.x + 2, fT + 11, FRIDGE.w - 4, 1);
    b.fillStyle = C.handle;
    b.fillRect(FRIDGE.x + FRIDGE.w - 5, fT + 5, 1, 5);
    b.fillRect(FRIDGE.x + FRIDGE.w - 5, fT + 14, 1, 12);
    b.fillStyle = C.bad; b.fillRect(FRIDGE.x + 4, fT + 4, 2, 1);
    b.fillStyle = C.on;  b.fillRect(FRIDGE.x + 8, fT + 4, 2, 1);
    b.fillStyle = C.wood5; b.fillRect(FRIDGE.x + 5, fT + 16, 6, 4);

    const kDy = Math.round(KITCHEN.d * DEPTH_RISE);
    const kDx = Math.round(KITCHEN.d * DEPTH_SHEAR);
    const kBack = (KITCHEN.y + KITCHEN.h) - KITCHEN.bh - kDy;

    const kc = box(KITCHEN, MAT.unit);
    const kT = faceTop(KITCHEN);
    for (const u of [0.07, 0.30]) {               // hobs, on the TOP face
      fillTopBand(b, kc, C.ink,  u,        u + 0.17, 0.26, 0.78);
      fillTopBand(b, kc, C.hob,  u + 0.02, u + 0.15, 0.34, 0.70);
      fillTopBand(b, kc, C.stl3, u + 0.06, u + 0.11, 0.46, 0.60);
    }
    fillTopBand(b, kc, C.stl1, 0.56, 0.94, 0.22, 0.82);
    fillTopBand(b, kc, C.stl4, 0.56, 0.94, 0.22, 0.28);
    fillTopBand(b, kc, C.ink,  0.61, 0.89, 0.32, 0.74);
    const tapX = KITCHEN.x + kDx + Math.round(KITCHEN.w * 0.75);
    b.fillStyle = C.stl5;
    b.fillRect(tapX, kBack - 8, 1, 9);
    b.fillRect(tapX - 5, kBack - 8, 6, 1);
    b.fillStyle = C.stl3; b.fillRect(tapX - 5, kBack - 6, 1, 2);

    const DOORS = 3, MARG = 2, GAP = 2;           // cupboards, on the FRONT face
    const dw = Math.floor((KITCHEN.w - MARG * 2 - GAP * (DOORS - 1)) / DOORS);
    for (let i = 0; i < DOORS; i++) {
      const dx0 = KITCHEN.x + MARG + i * (dw + GAP);
      b.fillStyle = C.chairA; b.fillRect(dx0, kT + 2, dw, KITCHEN.bh - 6);
      b.fillStyle = C.counterLit; b.fillRect(dx0, kT + 2, dw, 1);
      b.fillStyle = C.ink; b.fillRect(dx0 + dw - 3, kT + 5, 1, 4);
    }
    b.fillStyle = C.ink; b.fillRect(KITCHEN.x, kT + KITCHEN.bh - 3, KITCHEN.w, 3);

    box(SHELF, MAT.wood);
    const sT = faceTop(SHELF);
    // Three boards with books standing ON them. The old version was six
    // horizontal colour bars floating between two boards, and the bottom one
    // hung off the base of the unit. Widths are chosen to sum exactly to the
    // inner width, so nothing can run past the side.
    const shX = SHELF.x + 2, shW = SHELF.w - 4;
    const SPINE_W = [3, 2, 3, 2, 2];
    const SPINE_C = [C.book, C.bookB, C.bookC, C.bookB, C.book];
    for (let sh = 0; sh < 3; sh++) {
      const board = sT + 9 + sh * 10;
      b.fillStyle = C.wood2; b.fillRect(SHELF.x + 1, board, SHELF.w - 2, 1);
      b.fillStyle = C.wood1; b.fillRect(SHELF.x + 1, board + 1, SHELF.w - 2, 1);
      let x = shX;
      for (let i = 0; i < SPINE_W.length; i++) {
        const w2 = SPINE_W[i], h2 = 6 - ((i + sh) % 2);
        if (x + w2 > shX + shW) break;
        b.fillStyle = SPINE_C[(i + sh * 2) % SPINE_C.length];
        b.fillRect(x, board - h2, w2, h2);
        b.fillStyle = C.wood1; b.fillRect(x, board - h2, w2, 1);   // shadow, not a cap
        x += w2 + 1;
      }
    }

    // --- middle band -------------------------------------------------------
    box(TVST, MAT.wood);
    const vT = faceTop(TVST);
    for (let i = 0; i < 2; i++) {
      const pw = Math.floor((TVST.w - 6) / 2);
      const vx = TVST.x + 2 + i * (pw + 2);
      b.fillStyle = C.wood2; b.fillRect(vx, vT + 2, pw, TVST.bh - 5);
      b.fillStyle = C.wood5; b.fillRect(vx, vT + 2, pw, 1);
    }
    box(TV, MAT.steel);
    const tT = faceTop(TV);
    b.fillStyle = C.ink; b.fillRect(TV.x + 2, tT + 2, TV.w - 4, TV.bh - 6);
    b.fillStyle = C.on; b.fillRect(TV.x + TV.w - 4, tT + TV.bh - 3, 1, 1);
    // With the PC gone this screen is what he works at, so it is the terminal.
    TERMINAL = {x: TV.x + 3, y: tT + 3, w: TV.w - 6, h: TV.bh - 8};

  }

  // --- props Fred can be on ------------------------------------------------
  // These four are NOT baked into the background. Everything else in the room
  // is, because Fred can never be behind it -- but a piece he sits on or lies
  // in has to render partly behind him and partly in front, and a baked layer
  // can only ever be wholly behind. That single fact is why sitting looked
  // wrong at every seat coordinate I tried: there was no depth relationship to
  // get right, only a paint order that could not change.
  //
  // So each prop is split into the parts behind the occupant and the parts in
  // front, and rendering an occupied prop is a sandwich: back, occupant, front.
  // Unoccupied, the two halves just draw back to back and it is an ordinary
  // piece of furniture. This is the standard 2D answer -- y-sort by baseline
  // for the general case, with an explicit override for the case a single sort
  // key cannot express.
  function bedBack(g) {
    const bc = drawBox(g, BED.x, BED.y + BED.h, BED.w, BED.d, BED.bh, MAT.wood);
    // The bedding runs along the bed's LENGTH, because that is the way he lies
    // on it. It used to band across the depth axis, which split the mattress
    // front-to-back and read as the sheets being laid on sideways. Most of those
    // bands were also thinner than one row of the top face and rendered as
    // nothing at all.
    fillTopBand(g, bc, C.cloth2, 0.03, 0.97, 0.04, 0.96);            // sheet
    // Pillow placed under where the sleeping sprite's head actually lands
    // (blitted at BED.x+8, head at its columns 5-15), not at a guessed fraction.
    // Offset left by half the top face's shear. The pillow rides the sheared
    // face; the sleeping sprite is a flat blit that does not shear, so matching
    // them at the front edge leaves the pillow drifting right of his head at the
    // back. Half the shear splits the difference across the depth.
    fillTopBand(g, bc, C.pillow, 0.16, 0.60, 0.12, 0.88);
    fillTopBand(g, bc, C.cloth4, 0.16, 0.60, 0.12, 0.32);
    fillTopBand(g, bc, C.quilt,  0.64, 0.97, 0.06, 0.94);            // quilt, foot end
    fillTopBand(g, bc, "#8a5866", 0.64, 0.68, 0.06, 0.94);           // turned edge
    for (const u of [0.76, 0.87]) fillTopBand(g, bc, "#8a5866", u, u + 0.025, 0.06, 0.94);
  }

  function stoolBack(g) {
    const sc = drawBox(g, STOOL.x, STOOL.y + STOOL.h, STOOL.w, STOOL.d, STOOL.bh, MAT.chair);
    fillTopBand(g, sc, C.cloth2, 0.14, 0.86, 0.18, 0.82);
  }

  function plantBack(g) {
    drawBox(g, PLANT.x, PLANT.y + PLANT.h, PLANT.w, PLANT.d, PLANT.bh, MAT.wood);
    const py = PLANT.y + PLANT.h - PLANT.bh;
    g.fillStyle = C.onDim;
    for (const [dx, dy] of [[3,-5],[6,-10],[9,-7],[11,-3],[6,-3],[9,-11]])
      g.fillRect(PLANT.x + dx, py + dy, 2, 2);
    g.fillStyle = C.on;
    for (const [dx, dy] of [[6,-10],[9,-11]]) g.fillRect(PLANT.x + dx, py + dy, 2, 2);
  }

  const PROPS = {
    bed:   {P: BED,   back: bedBack,   front: null},
    stool: {P: STOOL, back: stoolBack, front: null},
    plant: {P: PLANT, back: plantBack, front: null},
  };
  // Which prop each settled activity puts him on.
  const OCCUPIES = {sleep: "bed", type: "stool", read: "stool"};

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
    const x = r.x, base = r.base;
    drawBox(ctx, x, base, RACK_W, RACK_D, RACK_H, MAT.steel);
    const top = base - RACK_H;

    // mounting rails down both faces of the chassis
    ctx.fillStyle = C.rail;
    for (let i = 0; i < 7; i++) {
      ctx.fillRect(x + 2, top + 8 + i * 6, 1, 2);
      ctx.fillRect(x + RACK_W - 3, top + 8 + i * 6, 1, 2);
    }

    // label plate, screwed to the chassis rather than floating above it
    const plateY = top + 3;
    ctx.fillStyle = C.stl1; ctx.fillRect(x + 4, plateY, RACK_W - 8, 7);
    ctx.fillStyle = C.stl2; ctx.fillRect(x + 4, plateY, RACK_W - 8, 1);
    ctx.fillStyle = burning.has(r.node) ? C.fMid : C.stl6;
    ctx.font = "6px ui-monospace, monospace"; ctx.textAlign = "center";
    ctx.fillText(r.node.replace("prxy-", ""), x + RACK_W / 2, plateY + 6);
    ctx.textAlign = "left";

    for (let u = 0; u < 5; u++) {
      const uy = top + 13 + u * 6;
      const g = r.guests[u];
      ctx.fillStyle = C.stl2; ctx.fillRect(x + 4, uy, RACK_W - 8, 5);
      ctx.fillStyle = C.stl4; ctx.fillRect(x + 4, uy, RACK_W - 8, 1);
      ctx.fillStyle = C.vent;
      for (let v = 0; v < 8; v++) ctx.fillRect(x + 7 + v * 2, uy + 2, 1, 2);
      ctx.fillStyle = C.handle; ctx.fillRect(x + 5, uy + 2, 1, 2);
      if (g) { ctx.fillStyle = C.stl5; ctx.fillRect(x + RACK_W - 9, uy + 1, 3, 3); }
    }

    if (burning.has(r.node)) {
      const seed = r.x * 31 + r.base;
      const wet = fightingHere(r) ? doused(nowMs, seed) : 1;
      drawFire(x, top, RACK_W, nowMs, seed, wet);
      pendingGlow.push([x, top - 9, RACK_W, seed, wet]);
      if (fightingHere(r)) pendingSteam.push([x + RACK_W / 2, top, seed]);
    }
  }

  // --- wall fittings ----------------------------------------------------------
  const DIGIT = {
    "0":["###","#.#","#.#","#.#","###"], "1":["..#","..#","..#","..#","..#"],
    "2":["###","..#","###","#..","###"], "3":["###","..#","###","..#","###"],
    "4":["#.#","#.#","###","..#","..#"], "5":["###","#..","###","..#","###"],
    "6":["###","#..","###","#.#","###"], "7":["###","..#","..#","..#","..#"],
    "8":["###","#.#","###","#.#","###"], "9":["###","#.#","###","..#","###"],
  };
  // --- neon ------------------------------------------------------------------
  // 5x7 with 1px strokes: a tube has a constant bore, so every stroke is one
  // pixel and the weight comes entirely from the glow around it. The old 3x5
  // blocks had no room for a counter in B or G, which is why they read as
  // stamped letters rather than bent glass.
  const NEON_GLYPH = {
    T: ["#####","..#..","..#..","..#..","..#..","..#..","..#.."],
    H: ["#...#","#...#","#...#","#####","#...#","#...#","#...#"],
    E: ["#####","#....","#....","####.","#....","#....","#####"],
    L: ["#....","#....","#....","#....","#....","#....","#####"],
    I: ["#####","..#..","..#..","..#..","..#..","..#..","#####"],
    G: [".###.","#...#","#....","#..##","#...#","#...#",".###."],
    A: [".###.","#...#","#...#","#####","#...#","#...#","#...#"],
    B: ["####.","#...#","#...#","####.","#...#","#...#","####."],
  };
  const NEON = {glow:"#ff3ba8", tube:"#ff7ad4", core:"#fff0fb", dead:"#4a3a56"};
  const NPAD = 4, NW = 5, NH = 7, NADV = 7, NSPACE = 4;   // 2px between tubes
  const SIGN_TEXT = "THE LIGHT LAB";
  // Which tubes have given up. The second L is dead outright; the I and the
  // final B stutter.
  const DEAD = new Set([10]);
  const FLICKER = new Set([1, 5, 8, 12]);

  // Each glyph is baked once: halo, then tube, then hot core. Per-frame this is
  // one drawImage at an alpha instead of ~500 fillRects, which is what makes a
  // real glow affordable at all.
  const neonCache = new Map();
  function bakeNeon(ch) {
    if (neonCache.has(ch)) return neonCache.get(ch);
    const rows = NEON_GLYPH[ch];
    const w = NW + NPAD * 2, h = NH + NPAD * 2;
    const mk = () => { const c = document.createElement("canvas"); c.width = w; c.height = h; return c; };
    const lit = mk(), dead = mk();
    const on = [];
    for (let r = 0; r < NH; r++)
      for (let c = 0; c < NW; c++)
        if (rows[r][c] === "#") on.push([c + NPAD, r + NPAD]);

    // Diamonds, not squares. Square halos from adjacent strokes tile into one
    // solid block, which floods the counters of B and G and turns a word into a
    // slab -- the falloff has to be shorter on the diagonal.
    const diamond = (g, x, y, rad) => {
      for (let dy = -rad; dy <= rad; dy++) {
        const wd = rad - Math.abs(dy);
        g.fillRect(x - wd, y + dy, wd * 2 + 1, 1);
      }
    };

    const gl = lit.getContext("2d");
    // Additive halo: overlapping falloffs pool light, so the inside of a letter
    // glows harder than its outside. That gradient is the whole effect.
    gl.globalCompositeOperation = "lighter";
    gl.fillStyle = NEON.glow;
    for (const [rad, al] of [[4, 0.030], [3, 0.040], [2, 0.055], [1, 0.085]]) {
      gl.globalAlpha = al;
      for (const [x, y] of on) diamond(gl, x, y, rad);
    }
    gl.globalAlpha = 0.22; gl.fillStyle = NEON.tube;    // bloom right at the glass
    for (const [x, y] of on) diamond(gl, x, y, 1);
    gl.globalCompositeOperation = "source-over";
    gl.globalAlpha = 1; gl.fillStyle = NEON.core;       // the tube itself
    for (const [x, y] of on) gl.fillRect(x, y, 1, 1);

    const gd = dead.getContext("2d");                   // unlit glass, still there
    gd.fillStyle = NEON.dead;
    for (const [x, y] of on) gd.fillRect(x, y, 1, 1);

    const out = {lit, dead};
    neonCache.set(ch, out);
    return out;
  }

  // How hard each tube is burning, 0..1. Not a per-frame coin flip: a failing
  // tube stutters in bursts and a dead one keeps trying to strike, which is
  // what makes it read as broken rather than simply absent.
  function tubeLevel(i, now) {
    if (DEAD.has(i)) {
      const win = Math.floor(now / 3300) + i * 7;
      if (hash2(i * 131, win) > 0.92) return hash2(i * 17, Math.floor(now / 70)) * 0.4;
      return 0;
    }
    const hum = 0.88 + 0.12 * Math.sin(now / 620 + i);   // mains ripple
    // Brownout: the whole sign sags together every so often, because they share
    // one tired transformer. Applied to healthy and failing tubes alike, which
    // is what makes them read as being on the same circuit.
    const bo = Math.floor(now / 5200);
    const brown = hash2(701, bo) > 0.80
      ? 0.45 + 0.3 * Math.sin(now / 40)
      : 1;
    if (!FLICKER.has(i)) return hum * brown;
    const burst = Math.floor(now / 1500) + i * 13;
    if (hash2(i * 97, burst) > 0.56) {
      return (hash2(i * 41, Math.floor(now / 45)) > 0.46 ? hum : 0.04) * brown;
    }
    return hum * brown;
  }

  function signTextWidth() {
    let w = 0;
    for (const ch of SIGN_TEXT) w += ch === " " ? NSPACE : NADV;
    return w - (SIGN_TEXT[SIGN_TEXT.length - 1] === " " ? NSPACE : NADV - NW);
  }

  function drawSign(now) {
    let x = SIGN.x + Math.round((SIGN.w - signTextWidth()) / 2);
    const y = SIGN.y + Math.round((SIGN.h - NH) / 2);
    for (let i = 0; i < SIGN_TEXT.length; i++) {
      const ch = SIGN_TEXT[i];
      if (ch === " ") { x += NSPACE; continue; }
      const g = bakeNeon(ch);
      const lvl = tubeLevel(i, now);
      // Unlit glass first, so a stuttering tube still has a body between blinks.
      ctx.drawImage(g.dead, x - NPAD, y - NPAD);
      if (lvl > 0.02) {
        ctx.globalCompositeOperation = "lighter";
        ctx.globalAlpha = Math.min(1, lvl);
        ctx.drawImage(g.lit, x - NPAD, y - NPAD);
        ctx.globalAlpha = 1;
        ctx.globalCompositeOperation = "source-over";
      }
      x += NADV;
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

  // Drapes hang in front of the glass and the weather, which is what sells them
  // as being inside the room rather than printed on the view.
  function drapes() {
    const x0 = WINDOW.x - 7, x1 = WINDOW.x + WINDOW.w + 7, top = WINDOW.y - 9;
    ctx.fillStyle = C.rod;                              // rod, with finials
    ctx.fillRect(x0 - 3, top, (x1 - x0) + 6, 2);
    ctx.fillStyle = C.stl4;
    ctx.fillRect(x0 - 4, top - 1, 3, 4); ctx.fillRect(x1 + 1, top - 1, 3, 4);

    const bot = WINDOW.y + WINDOW.h + 4;
    for (const [px, lean] of [[x0, 1], [x1 - 16, -1]]) {
      // Each panel is drawn as vertical folds: a pleat is a light column with a
      // dark one beside it, so the shading comes from the geometry rather than
      // from a gradient that would band at this size.
      for (let i = 0; i < 16; i++) {
        const fold = (i + (lean > 0 ? 0 : 1)) % 5;
        ctx.fillStyle = fold === 0 ? C.drapeLit : fold === 3 ? C.drapeDark : C.drape;
        // gathered at the rod, falling wider toward the floor
        const t = i / 15, sway = Math.round(Math.sin(t * 2.2) * 2) * lean;
        ctx.fillRect(px + i, top + 2, 1, bot - top - 2 + sway);
      }
      ctx.fillStyle = C.drapeDark;                      // hem
      ctx.fillRect(px, bot - 2, 16, 2);
      ctx.fillStyle = C.rod;                            // tieback
      ctx.fillRect(px + (lean > 0 ? 0 : 2), WINDOW.y + WINDOW.h - 14, 14, 2);
    }
    ctx.fillStyle = C.drape;                            // valance across the top
    ctx.fillRect(x0, top + 2, x1 - x0, 6);
    ctx.fillStyle = C.drapeLit; ctx.fillRect(x0, top + 2, x1 - x0, 1);
    ctx.fillStyle = C.drapeDark;
    for (let x = x0; x < x1; x += 4) ctx.fillRect(x, top + 6, 1, 2);
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
      // rate is MILLISECONDS PER PIXEL, so a bigger number is a slower cloud.
      // These were 15-34, which is ~100px/s -- a cloud crossed the window in
      // about a second. Now the nearest band takes ~40s to cross and the
      // highest ~75s, which is roughly what weather does. Wind still speeds it
      // up, but from a gentler divisor and capped, so a gale scuds rather than
      // teleports.
      const windMul = Math.min(2.2, 1 + ((weather && weather.wind_kph) || 0) / 50);
      ctx.fillStyle = kind === "fog" ? "#c7dcd0" : night ? "#3a4466" : C.cloud;
      // Nearer bands (larger oy) move faster: that is the parallax.
      for (const [oy, cw, rate] of [[4, 22, 580], [11, 28, 430], [19, 18, 310]]) {
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
    drapes();

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
  // Where he actually sits, rather than a point on the floor near the furniture:
  // the centre of a seat's top face, k rows back from its front edge. Derived
  // from the piece so it follows if the piece moves. The seated poses are drawn
  // with their legs cropped off and their bottom row on y, so y IS the seat.
  const seatOn = (P, k) => topPoint(P, 0.5, k / (Math.round(P.d * DEPTH_RISE) - 1));
  const DESK_SEAT = seatOn(STOOL, 1);
  const READ_SPOT = seatOn(STOOL, 1);   // same seat; only the pose differs
  const COOK_SPOT = {x: KITCHEN.x + 20, y: KITCHEN.y + KITCHEN.h + 18};  // stands
  let unacked = 0, tl = null, walking = false;
  // A rack burns while it has an unacked escalation -- which is what "that
  // server is on fire" actually means. Cleared by an ack or an all-clear.
  const burning = new Set();
  const IDLE_MS = 90000;
  let nowMs = 0;              // current frame time, for the fire's noise
  const pendingGlow = [];     // fire glows, collected during the sorted pass
  const pendingSteam = [];    // steam from water landing, drawn with the glows

  function inBunk() { return fred.x < BUNK.x + BUNK.w + 4; }

  // Replaced a hand-written aisle router. That one knew about the rack rows and
  // nothing else, so inside the studio he walked straight through the bed, the
  // desk and the counter -- there was no geometry for it to avoid.
  function pathTo(tx, ty) {
    return route(fred.x, fred.y, tx, ty, FRED_HW, FRED_FH, "fred", true);
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
    walk(BED.x + 24, BED.y + BED.h + 10, null);
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
    sleep: () => ({x: BED.x + 24, y: BED.y + BED.h + 10}),
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
    // Held at his lap, not his chin: the head is taller now, so the old height
    // put the book across his face in the seated crop.
    ctx.fillStyle = C.wood1; ctx.fillRect(fx + 6, fy - 7, 6, 4);
    ctx.fillStyle = flip ? C.book : C.bookB; ctx.fillRect(fx + 7, fy - 6, 4, 2);
  }

  function drawPot(now) {
    // On the left hob, placed through topPoint -- it used to be drawn at
    // KITCHEN.y + 8, which is the cupboard door.
    const hob = topPoint(KITCHEN, 0.10, 0.30);
    const PW = 7, PH = 5;
    ctx.fillStyle = C.stl1;  ctx.fillRect(hob.x, hob.y - PH, PW, PH);
    ctx.fillStyle = C.stl3;  ctx.fillRect(hob.x + 1, hob.y - PH, PW - 2, PH - 1);
    ctx.fillStyle = C.stl5;  ctx.fillRect(hob.x, hob.y - PH - 1, PW, 1);   // lid rim
    ctx.fillStyle = C.stl4;  ctx.fillRect(hob.x + 3, hob.y - PH - 2, 1, 1);
    ctx.fillStyle = C.steam; ctx.globalAlpha = 0.5;
    for (let i = 0; i < 3; i++) {
      const t = ((now / 620 + i * 0.33) % 1);
      ctx.fillRect(hob.x + 2 + i * 2 + Math.round(Math.sin(now / 400 + i)),
                   Math.round(hob.y - PH - 3 - t * 9), 1, 1);
    }
    ctx.globalAlpha = 1;
  }


  // --- solid geometry, collision and pathing ---------------------------------
  // A piece's floor footprint is NOT the rectangle it is drawn in. The drawn
  // rect covers the whole elevation; the thing you can walk into is the base
  // quad, which in this projection runs from (x, base-dy) at the back to
  // (x+w+dx, base) at the front. Its bounding box is the collision rect.
  //
  // Everything below works in that floor space. Sprites are tall and their
  // origin is at their feet, so an entity's footprint is a small rect sitting
  // just above its origin -- collide the feet, draw the body.
  function footprintOf(P) {
    const dy = Math.round(P.d * DEPTH_RISE), dx = Math.round(P.d * DEPTH_SHEAR);
    const base = P.y + P.h;
    // zTop is how high you would have to be to clear it. Everything here rests
    // on the floor, so a single top is enough -- no zBottom needed until
    // something has to pass underneath.
    return {x0: P.x, y0: base - dy, x1: P.x + P.w + dx, y1: base, zTop: P.bh};
  }

  // Where anything on the floor is allowed to be at all.
  const FLOOR = {x0: 12, y0: 80, x1: 468, y1: 280};

  let SOLIDS = [];
  // Rack tops are solid to a walker but landable by something airborne, so they
  // are kept separately as well as in SOLIDS.
  let PERCHES = [];

  function buildSolids() {
    // occupiable: furniture Fred is legitimately inside the footprint of when
    // he uses it. Sitting on the stool overlaps the desk edge too, because the
    // stool tucks under it. Named rather than indexed, so reordering the list
    // cannot silently retag the wrong piece.
    const PIECES = [
      [FRIDGE, false], [KITCHEN, false], [SHELF, false], [BED, true],
      [TVST, false], [STOOL, true], [PLANT, false],
    ];
    SOLIDS = PIECES.map(([P, occ]) => {
      const f = footprintOf(P);
      f.occupiable = occ;
      return f;
    });
    PERCHES = [];
    const rdy = Math.round(RACK_D * DEPTH_RISE), rdx = Math.round(RACK_D * DEPTH_SHEAR);
    for (const r of racks) {
      const box = {x0: r.x, y0: r.base - rdy, x1: r.x + RACK_W + rdx, y1: r.base,
                   zTop: RACK_H};
      SOLIDS.push(box);
      // The landable surface is the rack's top face, RACK_H above the floor.
      PERCHES.push({x0: r.x, y0: r.base - rdy, x1: r.x + RACK_W + rdx, y1: r.base,
                    z: RACK_H, rack: r, cx: r.x + RACK_W / 2, cy: r.base - 3});
    }
    // The partition, in two pieces so the doorway stays open.
    const px = BUNK.x + BUNK.w;
    SOLIDS.push({x0: px, y0: 60, x1: px + 4, y1: DOOR.y0, zTop: 1e9});   // walls
    SOLIDS.push({x0: px, y0: DOOR.y1, x1: px + 4, y1: 300, zTop: 1e9});  // never cleared
  }

  function hitsSolid(x0, y0, x1, y1) {
    for (let i = 0; i < SOLIDS.length; i++) {
      const s = SOLIDS[i];
      if (x0 < s.x1 && s.x0 < x1 && y0 < s.y1 && s.y0 < y1) return s;
    }
    return null;
  }

  // Can an entity with this footprint stand with its feet at (x, y)?
  function canStand(x, y, hw, fh, z) {
    if (x - hw < FLOOR.x0 || x + hw > FLOOR.x1 || y - fh < FLOOR.y0 || y > FLOOR.y1) return false;
    for (let i = 0; i < SOLIDS.length; i++) {
      const s = SOLIDS[i];
      if (!blocksZ(z || 0, 0, s)) continue;
      if (footOverlaps(x, y, hw, fh, s)) return false;
    }
    return true;
  }

  // --- movement ---------------------------------------------------------------
  const EPS = 0.01;

  const footOverlaps = (x, y, hw, fh, s) =>
    x - hw < s.x1 && x + hw > s.x0 && y - fh < s.y1 && y > s.y0;

  // Does this solid block something at height z with body height zh? Everything
  // rests on the floor, so only the top matters: clear a rack top and you are
  // over it. Passing z as 0 gives ordinary floor collision.
  const blocksZ = (z, zh, s) => z < (s.zTop === undefined ? 1e9 : s.zTop);

  // Axis-separated move with snap-to-contact. Resolving both axes at once and
  // then pushing out of the overlap sticks on corners and picks the wrong axis
  // when the two overlaps are close; per-axis resolution has no such ambiguity
  // because the direction is given by the sign of the movement. It is also what
  // produces sliding along a wall rather than stopping dead against it.
  function moveAxis(e, d, hw, fh, axis, z, zh) {
    if (!d) return;
    const x0 = e.x, y0 = e.y;
    if (axis === "x") e.x += d; else e.y += d;
    let target = axis === "x" ? e.x : e.y, hit = false;
    for (let i = 0; i < SOLIDS.length; i++) {
      const s = SOLIDS[i];
      if (!blocksZ(z, zh, s)) continue;
      if (!footOverlaps(e.x, e.y, hw, fh, s)) continue;
      // Already inside it before the move: it must NOT block, or anything that
      // ends up in geometry is trapped there permanently. This one line is the
      // difference between a recoverable glitch and a stuck character.
      if (footOverlaps(x0, y0, hw, fh, s)) continue;
      hit = true;
      if (axis === "x") {
        target = d > 0 ? Math.min(target, s.x0 - hw - EPS)
                       : Math.max(target, s.x1 + hw + EPS);
      } else {
        target = d > 0 ? Math.min(target, s.y0 - EPS)
                       : Math.max(target, s.y1 + fh + EPS);
      }
    }
    if (hit) { if (axis === "x") e.x = target; else e.y = target; }
    e.x = Math.max(FLOOR.x0 + hw, Math.min(FLOOR.x1 - hw, e.x));
    e.y = Math.max(FLOOR.y0 + fh, Math.min(FLOOR.y1, e.y));
  }

  // Sub-stepped so nothing can tunnel through a thin solid at speed. The step
  // is half the smaller footprint dimension, which costs nothing when slow.
  function slideTo(e, nx, ny, hw, fh, z, zh) {
    const dx = nx - e.x, dy = ny - e.y;
    const step = Math.max(1, Math.min(hw, fh) * 0.5);
    const n = Math.max(1, Math.ceil(Math.max(Math.abs(dx), Math.abs(dy)) / step));
    for (let i = 0; i < n; i++) {
      moveAxis(e, dx / n, hw, fh, "x", z || 0, zh || 0);
      moveAxis(e, dy / n, hw, fh, "y", z || 0, zh || 0);
    }
  }

  // If something ends up inside geometry -- a target chosen badly, a piece
  // moved under a sleeping cat -- walk outward in rings until a free cell is
  // found rather than leaving it permanently stuck.
  function nudgeFree(e, hw, fh) {
    if (canStand(e.x, e.y, hw, fh)) return true;
    for (let r = 2; r <= 48; r += 2) {
      for (let a = 0; a < 12; a++) {
        const t = (a / 12) * Math.PI * 2;
        const x = Math.round(e.x + Math.cos(t) * r), y = Math.round(e.y + Math.sin(t) * r);
        if (canStand(x, y, hw, fh)) { e.x = x; e.y = y; return true; }
      }
    }
    return false;
  }

  // --- grid A* ---------------------------------------------------------------
  // The room is small and static, so a uniform grid is the right tool: a navmesh
  // would be more work to build than the search costs to run. Obstacles are
  // inflated by the walker's own half-width when the grid is built (the
  // configuration-space trick), so the search can then treat the walker as a
  // point and never produce a path that clips a corner.
  const CELL = 4;
  const GRID = {};

  function buildGrid(key, hw, fh) {
    const w = Math.ceil((FLOOR.x1 - FLOOR.x0) / CELL);
    const h = Math.ceil((FLOOR.y1 - FLOOR.y0) / CELL);
    const open = new Uint8Array(w * h);
    for (let gy = 0; gy < h; gy++) {
      for (let gx = 0; gx < w; gx++) {
        const x = FLOOR.x0 + gx * CELL + CELL / 2;
        const y = FLOOR.y0 + gy * CELL + CELL / 2;
        open[gy * w + gx] = canStand(x, y, hw, fh) ? 1 : 0;
      }
    }
    GRID[key] = {w, h, open, hw, fh};
    return GRID[key];
  }

  const toCell = (x, y, g) => ({
    gx: Math.max(0, Math.min(g.w - 1, Math.floor((x - FLOOR.x0) / CELL))),
    gy: Math.max(0, Math.min(g.h - 1, Math.floor((y - FLOOR.y0) / CELL))),
  });
  const cellPos = (gx, gy) => ({x: FLOOR.x0 + gx * CELL + CELL / 2,
                                y: FLOOR.y0 + gy * CELL + CELL / 2});

  // Nearest open cell to a blocked one, so a target standing on furniture (a
  // seat, a counter) still produces a path that gets next to it.
  function nearestOpen(g, gx, gy) {
    if (g.open[gy * g.w + gx]) return {gx, gy};
    for (let r = 1; r < 24; r++) {
      for (let dy = -r; dy <= r; dy++) {
        for (let dx = -r; dx <= r; dx++) {
          if (Math.max(Math.abs(dx), Math.abs(dy)) !== r) continue;
          const nx = gx + dx, ny = gy + dy;
          if (nx < 0 || ny < 0 || nx >= g.w || ny >= g.h) continue;
          if (g.open[ny * g.w + nx]) return {gx: nx, gy: ny};
        }
      }
    }
    return null;
  }

  const DIRS = [[1,0,10],[-1,0,10],[0,1,10],[0,-1,10],
                [1,1,14],[1,-1,14],[-1,1,14],[-1,-1,14]];

  function astar(g, s, t) {
    const n = g.w * g.h;
    const gScore = new Int32Array(n).fill(0x7fffffff);
    const came = new Int32Array(n).fill(-1);
    const closed = new Uint8Array(n);
    const si = s.gy * g.w + s.gx, ti = t.gy * g.w + t.gx;
    const hEst = i => {
      const dx = Math.abs((i % g.w) - t.gx), dy = Math.abs(((i / g.w) | 0) - t.gy);
      // Octile, with Amit Patel's tie-break nudge (p < min-step-cost / max-path-
      // length). Without it A* expands a huge plateau of equal-f cells and the
      // pre-smoothing path wanders.
      return (10 * (dx + dy) + (14 - 20) * Math.min(dx, dy)) * 1.001;
    };
    gScore[si] = 0;
    // Binary heap, because a linear scan over ~5000 cells per pop is what makes
    // naive A* feel slow enough to skip frames.
    const heap = [[hEst(si), si]];
    const push = it => {
      heap.push(it); let i = heap.length - 1;
      while (i > 0) { const p = (i - 1) >> 1;
        if (heap[p][0] <= heap[i][0]) break;
        [heap[p], heap[i]] = [heap[i], heap[p]]; i = p; }
    };
    const pop = () => {
      const top = heap[0], last = heap.pop();
      if (heap.length) { heap[0] = last; let i = 0;
        for (;;) { const l = i * 2 + 1, r = l + 1; let m = i;
          if (l < heap.length && heap[l][0] < heap[m][0]) m = l;
          if (r < heap.length && heap[r][0] < heap[m][0]) m = r;
          if (m === i) break;
          [heap[m], heap[i]] = [heap[i], heap[m]]; i = m; } }
      return top;
    };
    let guard = 0;
    while (heap.length && guard++ < 20000) {
      const [, cur] = pop();
      if (cur === ti) break;
      if (closed[cur]) continue;
      closed[cur] = 1;
      const cx = cur % g.w, cy = (cur / g.w) | 0;
      for (const [dx, dy, cost] of DIRS) {
        const nx = cx + dx, ny = cy + dy;
        if (nx < 0 || ny < 0 || nx >= g.w || ny >= g.h) continue;
        const ni = ny * g.w + nx;
        if (!g.open[ni] || closed[ni]) continue;
        // No cutting corners diagonally: both orthogonal neighbours must be
        // open, or the walker clips the corner of a desk on the way past.
        if (dx && dy && (!g.open[cy * g.w + nx] || !g.open[ny * g.w + cx])) continue;
        const ng = gScore[cur] + cost;
        if (ng < gScore[ni]) { gScore[ni] = ng; came[ni] = cur; push([ng + hEst(ni), ni]); }
      }
    }
    if (came[ti] < 0 && ti !== si) return null;
    const out = [];
    for (let i = ti; i >= 0; i = came[i]) {
      out.push(cellPos(i % g.w, (i / g.w) | 0));
      if (i === si) break;
    }
    return out.reverse();
  }

  // Is the straight segment between two points walkable? Sampled at half a cell,
  // which is the resolution the grid was built at.
  function clearLine(ax, ay, bx, by, hw, fh) {
    const d = Math.hypot(bx - ax, by - ay), steps = Math.ceil(d / (CELL / 2));
    for (let i = 0; i <= steps; i++) {
      const t = steps ? i / steps : 0;
      if (!canStand(ax + (bx - ax) * t, ay + (by - ay) * t, hw, fh)) return false;
    }
    return true;
  }

  // String-pulling: keep only the corners the walker actually has to turn at.
  // Without it the path is a staircase of 4px steps and the walk reads as a
  // character shuffling along grid lines.
  function smooth(pts, hw, fh) {
    if (!pts || pts.length < 3) return pts || [];
    const out = [pts[0]];
    let i = 0;
    while (i < pts.length - 1) {
      let j = pts.length - 1;
      while (j > i + 1 && !clearLine(pts[i].x, pts[i].y, pts[j].x, pts[j].y, hw, fh)) j--;
      out.push(pts[j]);
      i = j;
    }
    return out;
  }

  // The one entry point: a walkable, smoothed route from a to b, or a direct
  // line if the search fails so nothing ever freezes waiting for a path.
  function route(ax, ay, bx, by, hw, fh, key, snapInto) {
    const g = GRID[key] || buildGrid(key, hw, fh);
    const s = nearestOpen(g, toCell(ax, ay, g).gx, toCell(ax, ay, g).gy);
    const t = nearestOpen(g, toCell(bx, by, g).gx, toCell(bx, by, g).gy);
    if (!s || !t) return [{x: bx, y: by}];
    if (clearLine(ax, ay, bx, by, hw, fh)) return [{x: bx, y: by}];
    const raw = astar(g, s, t);
    if (!raw) return [{x: bx, y: by}];
    const pts = smooth(raw, hw, fh);
    // Snap onto the exact target for the final hop. Interaction points are
    // deliberately inside solids -- a seat IS the chair -- so a route that
    // refused to enter geometry would always stop a few px short and every
    // seated pose would be off its furniture. Bounded so an unreachable target
    // cannot drag him through a wall: the last hop has to be a short one.
    const last = pts[pts.length - 1];
    if (last) {
      const near = Math.hypot(bx - last.x, by - last.y);
      // A target that is itself inside a solid is an interaction point -- a seat
      // IS the chair -- and always gets the final hop, or every seated pose ends
      // up beside its furniture. A target on open floor only gets it if the hop
      // is actually clear, because there the snap has no excuse to cut a corner
      // and walking is a tween along this path with no collision of its own.
      // Only Fred snaps into geometry, and only because his seats are inside
      // the furniture by construction. The cat asked for this too and it let
      // her settle down for a nap inside the armchair.
      const isInteraction = snapInto && !canStand(bx, by, hw, fh);
      if (near > 0.5 && near < 18 &&
          (isInteraction || clearLine(last.x, last.y, bx, by, hw, fh))) {
        pts.push({x: bx, y: by});
      }
    }
    return pts;
  }

  const FRED_HW = 6, FRED_FH = 5;
  const CAT_HW = 5, CAT_FH = 4;

  // --- the cat ---------------------------------------------------------------
  // She has her own little state machine, deliberately separate from Fred's:
  // she is not reacting to cluster events, she is just living here. The one
  // place the two meet is the mess -- she puts something on the floor and he
  // has to come and deal with it, which is the whole joke.
  const CAT = {
    // 14x9, facing right. Tail at the left, body in the middle, head clear of
    // the body at the right -- the first pass overlapped head and body and she
    // read as an orange loaf. Only the legs change between frames, which is all
    // a cat this size needs to look like it is walking.
    walkA: [
      "..a.......a.a.","..a......aaaaa","..aa....aBByBa","...a...aBBBBBa",
      "...aBBBBBBBBa.","..aBBBBBBBBBa.","..aeeeeeeeea..","...aa...aa....",
      "...aa...aa....",
    ],
    walkB: [
      "..a.......a.a.","..a......aaaaa","..aa....aBByBa","...a...aBBBBBa",
      "...aBBBBBBBBa.","..aBBBBBBBBBa.","..aeeeeeeeea..","..aa.....aa...",
      "..aa.....aa...",
    ],
    // Asleep. A curled cat is a comma with two ears on it.
    curl: [
      "..............","....a.a.......","...aaaaaa.....","..aBB--BBBa...",
      "..aBBBBBBBBa..","..aeeBBBBBBa..","...aaeeeeeaa..",".....aaaaa....",
      "..............",
    ],
    // Airborne: front legs forward, back legs trailing, body stretched. A cat
    // in the air is a longer shape than a cat on the ground.
    leap: [
      "..a.......a.a.","..aa.....aaaaa",".a.aa...aBByBa","a...aaaaBBBBBa",
      "....aBBBBBBBBa","..aaBBBBBBBBa.",".aa.aeeeeeea..","a.....aa..aa..",
      "..............",
    ],
    // Same curl, ears back and tail shifted. Even asleep a cat twitches.
    curlB: [
      "..............","....a.a.......","...aaaaaa.....","..aBB--BBBa...",
      "..aBBBBBBBBa..","..aeeBBBBBBa..","...aaeeeeeaa..","....aaaaa.....",
      "..............",
    ],
    // Front legs stretched out, back arched. Played on waking.
    stretch: [
      "..............","..........a.a.","..a......aaaaa","..aa....aBByBa",
      ".aaBBBBBBBBBBa","aeeBBBBBBBBBe.","..aaeeeeeeea..","...aa.....aa..",
      "..............",
    ],
    // Sat upright, tail curled round the feet.
    sit: [
      ".....a.a......","....aaaaa.....","....ayBya.....","....aBBBa.....",
      "...aBBBBBa....","...aBBBBBa....","...aBBBBBaa...","...aeeeeeaa...",
      "....aaaaa.....",
    ],
    // Same, tail flicked out. A sitting cat is never entirely still.
    sitB: [
      ".....a.a......","....aaaaa.....","....ayBya.....","....aBBBa.....",
      "...aBBBBBa..a.","...aBBBBBa.a..","...aBBBBBaa...","...aeeeeea....",
      "....aaaaa.....",
    ],
    // Washing: head down to the shoulder, one back leg up.
    groom: [
      ".....a.a......","....aaaaa.....","....aBBBa.....","...aBByBa.....",
      "..aBBBBBBa....","..aBBBBBBaa...","..aBBBBBBa.a..","..aeeeeeea....",
      "...aaaaa......",
    ],
  };

  const CAT_SPEED = 22;                      // px/sec, unhurried
  const CAT_RUN   = 62;                      // ... except when the ball moves
  const cat = {
    x: 120, y: 250, dir: 1, state: "prowl", until: 0, tx: 120, ty: 250,
    dist: 0, naps: 0,
    // Height above the floor. x and y stay the FLOOR position and never change
    // meaning -- collision, pathing and depth sorting all keep using them, and
    // only the draw call subtracts z. That is the whole trick to a jump in a
    // top-down view.
    z: 0, vz: 0, grounded: true, standingOn: null, prevZ: 0,
  };
  const GRAV = 900;              // px/s^2
  const JUMP_V0 = 330;           // reaches ~60px, a shade over a rack

  // The highest surface under her that she could be landing on. Only surfaces
  // she was above last frame count, so she cannot land on the side of a rack
  // she is passing.
  function groundUnder(e, hw, fh) {
    let g = 0, on = null;
    for (let i = 0; i < SOLIDS.length; i++) {
      const s = SOLIDS[i];
      if (s.zTop === undefined || s.zTop >= 1e9) continue;
      if (!footOverlaps(e.x, e.y, hw, fh, s)) continue;
      if (s.zTop > g && s.zTop <= e.prevZ + 1.5) { g = s.zTop; on = s; }
    }
    return {g, on};
  }

  function catGravity(dt) {
    cat.prevZ = cat.z;
    if (cat.grounded && cat.vz === 0) {
      // Still supported? Walking off the edge of a rack should drop her.
      const {g, on} = groundUnder(cat, CAT_HW, CAT_FH);
      if (g < cat.z - 0.5) { cat.grounded = false; cat.standingOn = null; }
      else { cat.z = g; cat.standingOn = on; return; }
    }
    cat.vz -= GRAV * dt / 1000;
    cat.z += cat.vz * dt / 1000;
    const {g, on} = groundUnder(cat, CAT_HW, CAT_FH);
    // Swept on the z axis: was she above it last frame and below it now? A bare
    // z <= g test misses the surface entirely on a fast fall.
    if (cat.vz <= 0 && cat.prevZ >= g && cat.z <= g) {
      cat.z = g; cat.vz = 0; cat.grounded = true; cat.standingOn = on;
    } else if (cat.z > g) {
      cat.grounded = false; cat.standingOn = null;
    }
    if (cat.z < 0) { cat.z = 0; cat.vz = 0; cat.grounded = true; cat.standingOn = null; }
  }
  // Where she is allowed to be: the studio floor and the near aisle, not inside
  // the racks and not through the back wall.
  const CAT_BOUNDS = {x0: 16, x1: 452, y0: 108, y1: 274};
  const ball = {x: 90, y: 262, vx: 0, vy: 0};
  let mess = null;                            // {x, y} once she has knocked one

  // Things worth knocking over. These are where the item LANDS -- on the floor,
  // clear of the piece it fell from. Using the item's own position put the
  // spill on the desk's front face, so Fred knelt there scrubbing a vertical
  // surface. Gravity applies to mugs.
  function knockables() {
    return [
      {x: topPoint(TVST, 0.80, 0.25).x, y: TVST.y + TVST.h + 7, what: "mug"},
      {x: PLANT.x - 8, y: PLANT.y + PLANT.h - 2, what: "plant"},
    ];
  }

  function catPick() {
    const r = Math.random();
    if (mess) return "prowl";                 // she is not sorry, but she is busy
    if (r < 0.24) return "play";
    if (r < 0.40) return "nap";
    if (r < 0.50) return "knock";
    if (r < 0.56) return "sit";
    if (r < 0.62) return "groom";
    if (r < 0.80 && PERCHES.length) return "leap";
    return "prowl";
  }

  function catGo(x, y) {
    cat.tx = Math.max(CAT_BOUNDS.x0, Math.min(CAT_BOUNDS.x1, x));
    cat.ty = Math.max(CAT_BOUNDS.y0, Math.min(CAT_BOUNDS.y1, y));
    cat.path = route(cat.x, cat.y, cat.tx, cat.ty, CAT_HW, CAT_FH, "cat");
    cat.pi = 0;
  }

  function catEnter(state, now) {
    cat.state = state;
    if (state === "prowl") {
      catGo(CAT_BOUNDS.x0 + Math.random() * (CAT_BOUNDS.x1 - CAT_BOUNDS.x0),
            CAT_BOUNDS.y0 + Math.random() * (CAT_BOUNDS.y1 - CAT_BOUNDS.y0));
      cat.until = now + 4000 + Math.random() * 5000;
    } else if (state === "play") {
      catGo(ball.x, ball.y);
      cat.until = now + 9000 + Math.random() * 6000;
    } else if (state === "nap") {
      catGo(BED.x + 30 + Math.random() * 90, 250 + Math.random() * 20);
      cat.until = now + 20000 + Math.random() * 25000;
    } else if (state === "sit" || state === "groom") {
      cat.until = now + 5000 + Math.random() * 7000;
    } else if (state === "leap") {
      // Pick a rack and walk to the floor directly in front of it; the jump
      // itself starts once she gets there.
      const p = PERCHES[Math.floor(Math.random() * PERCHES.length)];
      cat.perch = p;
      cat.launched = false;
      catGo(p.cx, p.y1 + 12);
      cat.until = now + 16000;
    } else if (state === "perch") {
      cat.until = now + 9000 + Math.random() * 9000;
    } else if (state === "knock") {
      const k = knockables()[Math.floor(Math.random() * 2)];
      cat.target = k;
      catGo(k.x, k.y);
      cat.until = now + 14000;
    }
  }

  function catStep(dt, now) {
    catGravity(dt);
    // Only unstick her on the floor: doing it mid-air would teleport her out of
    // a perfectly good jump.
    if (cat.grounded && cat.z === 0) nudgeFree(cat, CAT_HW, CAT_FH);

    if (cat.state === "leap") {
      const p = cat.perch;
      if (!p) { catEnter("prowl", now); return; }
      if (!cat.launched && cat.grounded &&
          Math.hypot(cat.x - p.cx, cat.y - (p.y1 + 12)) < 6) {
        cat.vz = JUMP_V0; cat.launched = true; cat.grounded = false;
        cat.tx = p.cx; cat.ty = p.cy;
      }
      if (cat.launched) {
        // Airborne: drive her horizontally toward the rack top. slideTo is
        // given her z, so once she is above the rack it stops blocking and she
        // sails over the footprint instead of bouncing off its side.
        const dx = cat.tx - cat.x, dy = cat.ty - cat.y, d = Math.hypot(dx, dy);
        if (d > 0.6) {
          const sp = 70 * dt / 1000, k = Math.min(1, sp / d);
          slideTo(cat, cat.x + dx * k, cat.y + dy * k, CAT_HW, CAT_FH, cat.z, 8);
          if (Math.abs(dx) > 0.3) cat.dir = dx > 0 ? 1 : -1;
        }
        if (cat.grounded) {
          if (cat.standingOn && cat.z > 4) catEnter("perch", now);
          else catEnter("prowl", now);
        }
        return;
      }
    }

    if (cat.state === "perch") {
      // Sitting on top of a rack. When the time is up, hop off the front.
      if (now > cat.until) {
        cat.vz = 150; cat.grounded = false;
        cat.tx = cat.x; cat.ty = (cat.standingOn ? cat.standingOn.y1 : cat.y) + 16;
        catEnter("leapdown", now);
      }
      return;
    }
    if (cat.state === "leapdown") {
      const dy = cat.ty - cat.y;
      if (Math.abs(dy) > 0.6) {
        const sp = 55 * dt / 1000;
        slideTo(cat, cat.x, cat.y + Math.sign(dy) * Math.min(sp, Math.abs(dy)),
                CAT_HW, CAT_FH, cat.z, 8);
      }
      if (cat.grounded && cat.z === 0) catEnter("prowl", now);
      return;
    }
    const d = Math.hypot(cat.tx - cat.x, cat.ty - cat.y);
    const moving = (cat.state === "prowl" || cat.state === "play" || cat.state === "knock" ||
                    cat.state === "leap" || (cat.state === "nap" && d > 2));
    if (moving && d > 1.5) {
      // Follow the routed waypoints rather than the target directly, and move
      // with slide so a bad path or a moving ball still cannot push her into
      // the furniture.
      if (!cat.path || !cat.path.length) catGo(cat.tx, cat.ty);
      const wp = cat.path[Math.min(cat.pi, cat.path.length - 1)] || {x: cat.tx, y: cat.ty};
      const wdx = wp.x - cat.x, wdy = wp.y - cat.y;
      const wd = Math.hypot(wdx, wdy);
      if (wd < 2.5 && cat.pi < cat.path.length - 1) cat.pi++;
      const sp = (cat.state === "play" ? CAT_RUN : CAT_SPEED) * dt / 1000;
      const k = wd > 0.01 ? Math.min(1, sp / wd) : 0;
      const px = cat.x, py = cat.y;
      slideTo(cat, cat.x + wdx * k, cat.y + wdy * k, CAT_HW, CAT_FH);
      cat.dist += Math.abs(cat.x - px) + Math.abs(cat.y - py);
      if (Math.abs(cat.x - px) > 0.15) cat.dir = cat.x > px ? 1 : -1;
      // Wedged against something with the waypoint unreachable: give up on this
      // route and pick another. Without this she grinds into a corner forever.
      if (Math.abs(cat.x - px) < 0.02 && Math.abs(cat.y - py) < 0.02) {
        cat.stuck = (cat.stuck || 0) + dt;
        if (cat.stuck > 700) { cat.stuck = 0; catEnter("prowl", now); }
      } else cat.stuck = 0;
    }

    if (cat.state === "play") {
      if (!cat.reroute || now > cat.reroute) { catGo(ball.x, ball.y); cat.reroute = now + 400; }
      if (d < 8 && Math.hypot(ball.vx, ball.vy) < 6) {
        const a = Math.random() * Math.PI * 2;   // a swipe, not a kick
        ball.vx = Math.cos(a) * (40 + Math.random() * 50);
        ball.vy = Math.sin(a) * (22 + Math.random() * 26);
      }
    }
    if (cat.state === "knock" && d < 6 && cat.target && !mess) {
      mess = {x: cat.target.x, y: cat.target.y, what: cat.target.what};
      cat.until = now;                        // job done, wander off
    }
    if (now > cat.until) catEnter(catPick(), now);
  }

  const BALL_R = 2, BALL_BOUNCE = 0.55, BALL_REST = 6;
  function ballStep(dt) {
    const f = Math.pow(0.12, dt / 1000);       // exponential drag, frame-rate safe
    // Sub-step so a hard swipe cannot pass through a desk between two frames.
    // At 4px radius against a 20px-deep footprint one step is fine at walking
    // speed and not at all fine at 200px/s.
    const dist = Math.hypot(ball.vx, ball.vy) * dt / 1000;
    const n = Math.max(1, Math.ceil(dist / BALL_R));
    for (let i = 0; i < n; i++) {
      const sdt = dt / n;
      let nx = ball.x + ball.vx * sdt / 1000;
      let ny = ball.y + ball.vy * sdt / 1000;

      // Resolve each axis against whatever it would enter and reflect only that
      // axis. Per-axis is what picks the correct face without computing a
      // normal: if the X move alone put it inside, it was a vertical face.
      const hx = hitsSolid(nx - BALL_R, ball.y - BALL_R, nx + BALL_R, ball.y + BALL_R);
      if (hx) {
        // Push back OUT to the face BEFORE flipping. Reflecting while still
        // overlapping is the classic jitter: it re-collides next frame, flips
        // again, and buzzes inside the wall forever.
        nx = ball.vx > 0 ? hx.x0 - BALL_R - EPS : hx.x1 + BALL_R + EPS;
        // ... and only reflect if actually approaching. Without this guard a
        // ball already separating gets flipped back inward.
        if (ball.vx * (hx.x0 - ball.x) > 0) {
          ball.vx = Math.abs(ball.vx) < BALL_REST ? 0 : -ball.vx * BALL_BOUNCE;
        }
      }
      const hy = hitsSolid(nx - BALL_R, ny - BALL_R, nx + BALL_R, ny + BALL_R);
      if (hy) {
        ny = ball.vy > 0 ? hy.y0 - BALL_R - EPS : hy.y1 + BALL_R + EPS;
        if (ball.vy * (hy.y0 - ball.y) > 0) {
          ball.vy = Math.abs(ball.vy) < BALL_REST ? 0 : -ball.vy * BALL_BOUNCE;
        }
      }
      ball.x = nx; ball.y = ny;

      if (ball.x < FLOOR.x0) { ball.x = FLOOR.x0; ball.vx = Math.abs(ball.vx) * 0.6; }
      if (ball.x > FLOOR.x1) { ball.x = FLOOR.x1; ball.vx = -Math.abs(ball.vx) * 0.6; }
      if (ball.y < FLOOR.y0) { ball.y = FLOOR.y0; ball.vy = Math.abs(ball.vy) * 0.6; }
      if (ball.y > FLOOR.y1) { ball.y = FLOOR.y1; ball.vy = -Math.abs(ball.vy) * 0.6; }
    }
    ball.vx *= f; ball.vy *= f;
    if (Math.abs(ball.vx) < 0.6) ball.vx = 0;
    if (Math.abs(ball.vy) < 0.6) ball.vy = 0;
    // Last resort: something moved onto it, or a swipe launched it from an
    // overlapping spot. Walk it out rather than leave it embedded.
    if (hitsSolid(ball.x - BALL_R, ball.y - BALL_R, ball.x + BALL_R, ball.y + BALL_R)) {
      nudgeFree(ball, BALL_R, BALL_R);
    }
  }

  function drawCat(now) {
    const surf = cat.standingOn ? cat.standingOn.zTop : 0;
    const air = Math.max(0, cat.z - surf);
    // The shadow sits on whatever is UNDER her, not on the floor, and shrinks
    // with the gap. Without it a jumping cat is indistinguishable from a cat
    // that walked up-screen -- the shadow is the only thing carrying height.
    const sc = Math.max(0.4, 1 - air / 46);
    ctx.fillStyle = "rgba(11,10,16," + (0.42 * sc).toFixed(2) + ")";
    const sw = Math.round(11 * sc), sy = Math.round(cat.y - surf);
    ctx.fillRect(Math.round(cat.x) - (sw >> 1), sy - 1, sw, 2);

    // Draw at (x, y - z): only the sprite moves up, the floor position does not.
    const x = Math.round(cat.x) - 7, y = Math.round(cat.y) - 8 - Math.round(cat.z);
    if (cat.state === "nap" && Math.hypot(cat.tx - cat.x, cat.ty - cat.y) < 3) {
      // A twitch every few seconds, and a proper stretch on the way out of it.
      const left = cat.until - now;
      const frame = left < 1400 ? CAT.stretch
                  : (Math.floor(now / 2300) % 4 === 0 ? CAT.curlB : CAT.curl);
      blit(frame, x, y, cat.dir < 0);
      if (Math.floor(now / 1100) % 2) {
        ctx.fillStyle = C.dim; ctx.font = "5px ui-monospace, monospace";
        ctx.textAlign = "left"; ctx.fillText("z", x + 13, y - 1);
      }
      return;
    }
    if (cat.state === "groom") {
      blit(Math.floor(now / 320) % 2 ? CAT.groom : CAT.sit, x, y, cat.dir < 0);
      return;
    }
    if (cat.state === "sit" || cat.state === "perch") {
      // Tail flicks on a slow, irregular beat rather than a metronome.
      const flick = (Math.floor(now / 700) % 5) === 0;
      blit(flick ? CAT.sitB : CAT.sit, x, y, cat.dir < 0);
      return;
    }
    if (!cat.grounded) { blit(CAT.leap, x, y, cat.dir < 0); return; }
    // Distance-driven, same as Fred: time-driven legs slide when speed changes.
    const f = (Math.floor(cat.dist / 5) & 1) ? CAT.walkB : CAT.walkA;
    blit(f, x, y, cat.dir < 0);
  }

  function drawBall() {
    const x = Math.round(ball.x), y = Math.round(ball.y);
    ctx.fillStyle = C.ballA; ctx.fillRect(x - 2, y - 4, 4, 4);
    ctx.fillStyle = C.ballB; ctx.fillRect(x - 1, y - 4, 2, 1);
    ctx.fillStyle = "rgba(11,10,16,.35)"; ctx.fillRect(x - 2, y - 1, 4, 1);
  }

  function drawMess(now) {
    if (!mess) return;
    const x = Math.round(mess.x), y = Math.round(mess.y);
    if (mess.what === "mug") {
      ctx.fillStyle = C.wood3; ctx.fillRect(x - 2, y - 3, 5, 3);   // mug on its side
      ctx.fillStyle = C.mess;  ctx.fillRect(x - 5, y, 12, 2);      // and the spill
      ctx.fillRect(x - 3, y - 1, 7, 1);
    } else {
      ctx.fillStyle = C.wood2; ctx.fillRect(x - 3, y - 3, 7, 3);
      ctx.fillStyle = C.mess;  ctx.fillRect(x - 6, y, 13, 2);      // soil
      ctx.fillStyle = C.onDim; ctx.fillRect(x + 2, y - 2, 2, 2);
    }
    if (Math.floor(now / 700) % 2) {                                // a small marker
      ctx.fillStyle = C.bad; ctx.fillRect(x, y - 7, 1, 3);
      ctx.fillRect(x, y - 3, 1, 1);
    }
  }

  // --- fire, and the fighting of it -----------------------------------------

  // Two octaves of the stable hash: a slow one that makes the flame writhe and a
  // fast one that makes it crackle. One octave alone either strobes or crawls.
  function flameHeight(seed, i, w, now) {
    const slow = hash2(seed + i * 7, Math.floor(now / 110));
    const fast = hash2(seed + i * 131, Math.floor(now / 55));
    const taper = 1 - Math.abs((i / Math.max(1, w - 1)) * 2 - 1);   // tallest mid-rack
    return Math.round((2 + (slow * 0.65 + fast * 0.35) * 11) * (0.3 + taper));
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

  // Restored. 7b9711b deleted this whole subsystem but left every call site in
  // place, so drawRack and the fight pose both threw on any burning rack: the
  // room rendered nothing at all during an escalation, which is the one moment
  // it has a job to do. Found by rendering the states rather than the happy path.

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

    // The prop he is currently on rides with him instead of sorting on its own,
    // because "inside" is not a position on the depth axis -- see PROPS.
    const onProp = walking ? null : OCCUPIES[fred.activity];
    for (const key of Object.keys(PROPS)) {
      if (key === onProp) continue;
      const pr = PROPS[key];
      items.push({sortY: pr.P.y + pr.P.h, sortX: pr.P.x, prop: pr});
    }
    // Standing on a rack her floor y is behind the rack's own baseline, so the
    // rack would draw after her and hide her. Riders inherit their platform's
    // sort key -- the standard 2.5D rule.
    items.push({sortY: cat.standingOn ? cat.standingOn.y1 + 0.5 : cat.y,
                sortX: cat.x, cat: true});
    items.push({sortY: ball.y, sortX: ball.x, ball: true});
    if (mess) items.push({sortY: mess.y, sortX: mess.x, spill: true});

    const rider = onProp ? PROPS[onProp] : null;
    items.push({
      sortY: rider ? rider.P.y + rider.P.h : fred.y,
      sortX: fred.x, fred: true, prop: rider,
    });
    items.sort((a, b) => (a.sortY - b.sortY) || (a.sortX - b.sortX));

    for (const it of items) {
      if (it.cat)   { drawCat(now);  continue; }
      if (it.ball)  { drawBall();    continue; }
      if (it.spill) { drawMess(now); continue; }
      if (it.prop && !it.fred) {          // unoccupied: the halves just meet up
        it.prop.back(ctx);
        if (it.prop.front) it.prop.front(ctx);
        continue;
      }
      if (!it.fred) { drawRack(it.r); continue; }
      if (it.prop) it.prop.back(ctx);     // ... and the sandwich when he is on it
      if (fred.asleep) {
        const rise = Math.floor(now / 1000) % 2 ? -1 : 0;
        blit(SLEEP, BED.x + 8, BED.y + 1 + rise, false);
        if (it.prop && it.prop.front) it.prop.front(ctx);
        continue;
      }
      // No floor shadow when he is on a prop -- it landed on the seat, not the
      // floor. Riding a prop is exactly the condition, so ask that.
      if (!it.prop) {
        ctx.fillStyle = "rgba(11,10,16,.45)";
        ctx.fillRect(Math.round(fred.x) - 7, Math.round(fred.y) - 2, 14, 3);
      }

      // A settled Fred still breathes. Two frames, ~1.5s apart: any faster and
      // it reads as a twitch, any smaller than a whole pixel is not expressible.
      // This is the one thing that separates "idle" from "paused".
      const breath = Math.floor(now / 1500) % 2 ? -1 : 0;

      // Standing still is the least informative thing he can do, so every
      // arrival hands off to a pose that says what he is actually doing.
      if (!walking && fred.activity === "read") {
        blit(withBlink(SPR.side, "side", now).slice(0, 21),
             fred.x - 9, fred.y - 21 + breath, true);
        drawBook(fred.x - 9, fred.y + breath, now);
      } else if (!walking && fred.activity === "clean") {
        // Crouched over it, scrubbing. The cloth is the only moving part.
        const sweep = Math.floor(now / 220) % 2;
        blit(withBlink(SPR.side, "side", now).slice(0, 22), fred.x - 9, fred.y - 20, fred.flip);
        if (mess) {                       // the cloth works the spill, not his lap
          ctx.fillStyle = C.cloth4;
          ctx.fillRect(mess.x - 4 + sweep * 3, mess.y - 2, 6, 2);
          ctx.fillStyle = C.cloth3;
          ctx.fillRect(mess.x - 4 + sweep * 3, mess.y - 2, 6, 1);
        }
      } else if (!walking && fred.activity === "cook") {
        // Slower than typing, and the whole body rocks with it -- stirring is a
        // shoulder movement, not a wrist one.
        const stir = Math.floor(now / 300) % 2;
        const h = ["a", "b"][Math.floor(now / 300) % 2];
        blit(withHands(SPR.up, h, "up"), fred.x - 9, fred.y - 27 - stir, false);
      } else if (!walking && fred.activity === "type") {
        // In profile facing left, because the telly is to his left now. Facing
        // away from the screen he is supposedly working at read as him ignoring
        // it. The stretch has its own profile frame for the same reason -- the
        // back-view one would have snapped him ninety degrees for two seconds.
        if (now % 26000 < 1700) {
          blit(SPR.sideStretch.slice(0, 21), fred.x - 9, fred.y - 22 + breath, true);
        } else {
          const h = ["a", "b"][Math.floor(now / 150) % 2];
          const rows = withHands(withBlink(SPR.side, "side", now), h, "side");
          blit(rows.slice(0, 21), fred.x - 9, fred.y - 21 + breath, true);
        }
      } else if (!walking && fred.activity === "inspect") {
        // Checking a rack: he leans in and out, and one hand comes up to it.
        const lean = Math.floor(now / 260) % 2;
        const h = (Math.floor(now / 520) % 2) ? "a" : "rest";
        blit(withHands(SPR.up, h, "up").slice(0, 24), fred.x - 9, fred.y - 24 + lean, false);
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
        // Arms swing on the same stride parity as the legs, so the swing is
        // locked to his feet rather than drifting against them.
        const swing = (Math.floor(fred.dist / 6) & 1) ? "a" : "b";
        const base = withHands(withBlink(SPR[key], key, now),
                               moved > 0.05 ? swing : "rest", key);
        const rows = apart ? base.slice(0, 21).concat(LEGS_APART[key]) : base;
        const rise = apart ? -1 : (moved > 0.05 ? 0 : breath);
        blit(rows, fred.x - 9, fred.y - 27 + rise, fred.flip);
      }
      if (it.prop && it.prop.front) it.prop.front(ctx);
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
  let running = true, lastT = 0, dtMs = 33, cleanUntil = 0;
  function frame(now) {
    requestAnimationFrame(frame);
    if (!running || now - lastT < MIN_DT) return;
    // Derived from the rAF timestamp, not assumed: on a 144Hz display a fixed
    // step would run the weather 2.4x too fast. Clamped so a backgrounded tab
    // does not teleport every raindrop on return.
    dtMs = Math.min(now - lastT, 120);
    lastT = now;
    if (A) A.engine.update();          // advance tweens exactly once, then draw
    catStep(dtMs, now);
    ballStep(dtMs);

    // Going to bed is latched rather than gated on !walking: the old form meant
    // that if a walk ever failed to signal completion -- a cancelled timeline,
    // or the loop being paused mid-stride while the tab sat on another view --
    // he stayed on his feet indefinitely with nothing to retry it.
    // Idle behaviour is re-decided against the clock, not latched once. Gating
    // this on "not asleep" meant that once he went to bed nothing ever
    // reconsidered, so he slept straight through the following day.
    // A mess outranks the routine but not a fire: if a rack is alight he has
    // bigger problems than the cat, and the spill can wait.
    if (mess && !burning.size && !walking && fred.activity !== "clean") {
      fred.say = ""; fred.asleep = false;
      // Stamped on arrival, not on departure: closing over this frame's `now`
      // charged the walk against his scrubbing time, so a long walk left him
      // wiping for a fraction of a second.
      // Beside it, not in front of it: standing directly below put his head
      // over the spill. Which side depends on what else is there -- on the left
      // of the room the stool sorts in front of him and swallows his legs, so
      // he works from the open side and faces the mess.
      const side = mess.x < 92 ? 1 : -1;
      walk(mess.x + 13 * side, Math.min(mess.y + 9, BUNK.y + BUNK.h - 8), () => {
        fred.flip = side > 0;
        fred.activity = "clean";
        cleanUntil = (window.performance ? performance.now() : now) + 5200;
      });
    } else if (fred.activity === "clean" && now > cleanUntil) {
      mess = null;
      // Must leave the clean state before walking away. Left as "clean" this
      // branch re-fired every frame, restarting the walk, and he never arrived
      // anywhere again.
      fred.activity = "walk";
      goIdle();
    }

    const idle = Date.now() - fred.lastEvent;
    if (idle > IDLE_MS && !walking && fred.activity !== "clean" && !mess) {
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
  if (typeof window !== 'undefined') {
    window.__forceIdle = () => { fred.lastEvent = 0; };
    // Read-only view of what everything thinks it is doing. Handy from a console
    // when the room is misbehaving, and it is what the state harness polls.
    window.__roomProbe = () => ({
      fred: fred.activity, walking, asleep: fred.asleep,
      cat: cat.state, mess: mess ? mess.what : null,
      burning: burning.size,
    });
    window.__catKnock = () => { catEnter("knock", performance.now ? performance.now() : 0); };
    // Physics assertions for the harness: is anything currently overlapping a
    // solid? Nothing should ever be, at any point in a long run.
    window.__catLeap = () => { catEnter("leap", performance.now()); return true; };
    window.__rackHeight = () => RACK_H;
    window.__ballSet = (x, y, vx, vy) => { ball.x = x; ball.y = y; ball.vx = vx; ball.vy = vy; };
    window.__ballState = () => ({x: ball.x, y: ball.y, vx: ball.vx, vy: ball.vy});
    window.__physics = () => ({
      // Seated or asleep he is deliberately inside a prop -- a seat IS the
      // chair -- so overlapping only counts as a fault when he is on his feet.
      // He is only at fault inside something he can never legitimately be in.
      // Overlapping the stool, chair or bed is what sitting down and getting up
      // both look like; overlapping a rack or the counter is a bug.
      fred: {x: Math.round(fred.x), y: Math.round(fred.y), act: fred.activity,
             bad: SOLIDS.some(s2 => !s2.occupiable &&
                    footOverlaps(fred.x, fred.y, FRED_HW, FRED_FH, s2))},
      cat:  {x: Math.round(cat.x), y: Math.round(cat.y), z: Math.round(cat.z || 0),
             st: cat.state, stuck: Math.round(cat.stuck || 0),
             onRack: !!(cat.standingOn && cat.standingOn.zTop === RACK_H),
             grounded: cat.grounded,
             bad: !(cat.z > 0) && !canStand(cat.x, cat.y, CAT_HW, CAT_FH, cat.z || 0)},
      ball: {x: Math.round(ball.x), y: Math.round(ball.y),
             bad: !!hitsSolid(ball.x - 2, ball.y - 2, ball.x + 2, ball.y + 2)},
      solids: SOLIDS.length,
    });
  }
  load().then(() => requestAnimationFrame(frame));
  setInterval(load, 60000);
})();
