# CLAUDE.md — Midway Quest

Read this first in every session. It is the shared memory of how this project works and what has already gone wrong.

## What this is

**Midway Quest** is a 2D pixel-art town game for the internal portal **Midway** (org: ktbgs), module **KPlay**.
Goal: make security awareness, ESG (waste sorting, CO2) and team spirit fun, so employees come back daily.
Mascot: **Paksa**, a wind bird (วายุ theme). All in-game text is **Thai**; keep new UI text in Thai.

Owner: Krit (full-stack, Go Fiber / React-TS / Postgres / Redis). Prefers simple solutions one person can maintain.
Reply to him in Thai, concise.

## Repo layout

```
game/index.html        the whole game: one self-contained HTML file (~420 KB, ~2,900 lines of JS)
tools/smoke-test.js    headless boot + walk-around test (Node + @napi-rs/canvas)
api/                   Go Fiber + Postgres + Redis backend (not wired to the game yet, see api/README.md)
docs/BACKLOG.md        what's next
```

Run the game: open `game/index.html` in a browser (no build step). Run the test: `cd tools && npm i && npm run smoke`.
**Run the smoke test after every change.** It boots the game, visits every district and interior, day and night,
and exits non-zero if the in-game error box appears.

## How the single file is organised

Everything is inside one `<script>` IIFE. Order matters because of `const`/`let` (see pitfalls).
Approximate section order (search for the `/* ---- name ---- */` comments):

1. **sprites** — all art drawn in code with helpers `cv(w,h)`, `R(ctx,col,x,y,w,h)`, `pcirc`, `pell`, `outline`, `flipH`, `shade(hex,k)`. No image files.
2. **world layout** — 168×136 tiles (`T=16`, `MW`, `MH`). Grids: `G` (ground type), `SOL` (solid), `OCC` (reserved, no trees), `RIV`, `BRG`, `HGT`/`CLIFF` (terraces).
   Ground types: 0 grass, 1 path, 2 plaza, 3 water, 4 forest, 5 paddy, 6 bridge/pier, 7 dike, 8 neon path, 9 sea, 10 sand.
   Helpers: `fillG`, `mark(arr,x0,y0,x1,y1)`, `place(img,tx,ty,w)`, `add({img,x,y,base,...})`. Many later "world" inserts sit **just before** `const TREES={...}` so trees avoid them.
   **World rebuild (map v2):** the town code still places everything in the *old 128×112 coordinates*. `worldRebuild()` (search `world rebuild`, runs once at load,
   then `WORLD_V=2`) moves each district by its offset in `DIST`, generates height tiers / cliffs / river / dam / beach on 168×136, adds the new places
   (เขื่อนวายุ + hydro, ถ้ำคริสตัล, สวนพฤกษาเรือนกระจก), routes the road network out from the plaza, then plants flora. Old-space → new-space helpers:
   `TP(x,y)` (px), `TPT(tx,ty)` (tiles), `TOFF`, `DBOX(name)`. **New code placed in old space must be listed in the transplant step**, and runtime literals
   (zone checks, spawn points) must go through `TP`/`TPT` or use new-space numbers. `WORLD_V` is a `var` declared next to `MW` (TDZ, pitfall 1).
   Non-RIV ground 3 is a sprite pond (not drawn by the tile painter); generated water sets `RIV=1`.
3. **objects + spatial buckets**, **decals** (`DEC.push({img,x,y})`), **chunked ground renderer**, **minimap**.
4. **entities / critters**, **state + HUD** (`S` = player state), **dialog** (`say([...])`), **content** (quests `Q`, achievements `ACH`), **HQ interior**, **interiors** (`roomBase`, `SCENES`, `DOORS`).
5. **features** — one block per system, appended over time (fishing, waste sorting, raid boss, shops, inventory, forest, farming, kite, windmill, ranch, beach/island, museum, Hall of Fame, PC controls, audio, shuttle, HQ floor 2, solar farm, economy, team fund, retention, office, secret merchant…).
6. **emotes**, **online** (presence via artifact runtime), **input**, **map overlay**, **ambience**, **loop**.

### Extension points (use these, don't edit the core loop)
- `HOOKS = {town, post, interior, ipost, top, update}` — push callbacks. `town/interior` receive `L` (y-sorted draw list): `L.push({base:y, fn:()=>{...}})`.
  `top` draws after the night overlay (use for things that must glow at night). Each hook call is wrapped in try/catch → `hookErr`.
- `INT` (town) / `SCENES[k].INT` — interactables `{x,y,r,my,label,act,mark?,off?}`. `label` shows on the prompt ("SPACE คุย").
- `SCENES[k]` — interior scenes `{W,H,cols,rows,sol,ground,OBJ,npcs,INT,doorX,exitTo,zone,under?,noExit?,warps?}`.
- `ITEMS` via `reg(id,name,cat,iconCanvas,desc,sellPrice,useFn,useLabel)`; give items with `addItem(id,n)`.
- `SHOPS[key] = {t,sub,items:[[id,price,qty?]],cur?}` then `openShop(key)`.
- Rewards: `gain(xp,coins)`; quest progress `prog(id)`; popups `pop(text,color)`, `banner(title,sub)`, `burst(x,y,colors,n)`; sound `sfx(name)`.
- Economy knobs: `CFG` (defaults `CFG_DEF`), editable in-game at Map → "ตั้งค่าเศรษฐกิจเกม" (admin/testing panel, local only).

### Map layout overrides
Positions placed in code can be moved without editing code: Map → "จัดวางแผนที่ (แอดมิน)" (or open `index.html?edit`).
Drag objects, then download `layout.json` or publish it to GitHub (commits `game/layout.json` → Pages redeploys).
Keys are each object's original `x,base,width`, so **moving an object in code orphans its layout entry** (the editor reports skipped entries).
**System points** (`LAY.P`, registered in `layInit`): coordinates a system reads at runtime (bus stop arrival `STOP_POS`,
`RUN_CP`, interior `exitTo`, gypsy spots, feathers, crystals, mushrooms). They follow their host object and can be dragged on their own.
**When a new system stores its own coordinates, register them in `layInit`**, or moving the object in the editor will leave that system behind.
Decals whose position is a code constant (`FX`, `PX`, `LX`, `AX`, pier, badminton court) are locked.
Hidden objects stay in `OBJ` (flag `o.hide`); never splice `OBJ` from the editor. `layout.json` is only fetched over http(s), not `file://`.

### PNG art overrides
Sprites are drawn in code, but any canvas wrapped in `artReg('name', makeX())` can be replaced by a PNG listed in
`game/assets/manifest.json` (`{"name": "set/file.png"}`). The PNG is painted into the existing canvas, so it **must keep the
original sprite size** (positions, collisions and `layout.json` stay valid). `?art=0` shows the code-drawn art for comparison.
**HD (2x) art:** ground chunks are painted at 2x when the HD grass set (`grass_1`, assets/T01_grass, 32px tiles) is present, so 32px PNG tiles keep full detail; new art should be 2x the sprite size (see the HD prompt packs).
Object sprites whose PNG is an exact integer multiple (2x, 3x…) of the canvas keep the full-res image as `canvas._hd` (set in `artPaint`); the world draw lists (town + interior) draw `_hd` at the logical size, so HD buildings stay sharp at zoom. Flipped art (`artRegFlip`) and baked canvases (interior ground, minimap) still use the downscaled canvas. The `_hd` path needs the PNG to be an **exact** multiple in both axes (check the real canvas size: `makeHQ` is 242×150, not 242×149).
**`ART_FITW`** (next to `artPaint`): names whose PNG keeps its own proportions; the canvas keeps its width, takes the PNG's height, and every object using it shifts so its base stays put. Use it when new art has a different aspect than the code sprite, instead of stretching the PNG to the old size (set 03 buildings do this).
**Corner autotiles** (`CORNER_SETS`, `gcls`, `paintCorners`; art in `assets/G02..G13`): HD terrain sets named `<name>_<mask>.png` (mask bits NW=1 NE=2 SW=4 SE=8 = neighbouring tile is that set, `_15b/c/d` fill variants) are drawn on tile vertices (half-tile offset) over a grass base, giving rounded organic edges. `gcls(x,y)` maps a tile to its set: path, plaza, **market** (plaza tiles inside the market district), forest, paddy, soil (dike), neon, sand, sea (`sea_deep_*` offshore), water (river `RIV` + bridge tiles), beach pier → sea/sand. `CORNER_SETS` is the draw order (the art pack's layer order); complementary masks of two sets tile without gaps. With these sets loaded `hdTerrain` skips its own river banks and sand/path borders. Grass: `grass_high_*` on terraces (`HGT>0`), `grass_dark_*` next to forest.
Round plaza and arena are decals with PNG overrides (`plaza_round`, `arena_round`, 2x); decals draw `img._hd` too. The fountain bowl is its own decal on top. Waterfalls (`wfArt`, `assets/G07_waterfall`, 3 frames): drop runs from `WATERFALLS` widened to the river width, drawn lip/fall/fall/splash; the run below the dam uses `dam_spill`.
**Cliffs** (`paintCliffs`, `paintStairs`, `assets/G09_cliff`): dual-grid pieces 16×32 per height level (bit = tile `HGT>=L`), drawn before decals; a south edge hangs a 1.5-tile face over the `CLIFF` 1/3 rows (row 3 becomes walkable once the set loads), side/north edges stay open where a road or `RAMP` crosses. 2–3 wide stair flights use `stairs_2w/3w`; 1-wide steps keep the code stairs. With the set loaded `hdTerrain` skips its contour cliffs (terrace light stays).
**Market square** is path ground under an old checker decal (`noArt:'market_15'` hides it); `gcls` maps that rectangle's path tiles to `market`.
**Ranch fence** (`fenceArt`, `assets/G11_fences`, masks N=1 E=2 S=4 W=8): fence objects swap to the matching piece once the set loads (canvas stays 18×20, piece kept as `_hd`, no baked shadow).
**Piece decals** (`pieceDecal`, `artDecal`): a decal with `artOnly:'<piece>'` is composed at 2x from PNG pieces (`fill(g)`) the first time that piece is loaded; the code-drawn twin carries `noArt:'<piece>'` and hides. Used for bridges (`assets/G10_bridges`: abutment on each bank + `mid_1/2` deck pieces, 3 tiles across the road, built per crossing in `worldRebuild` 7b) and the beach pier.
**Animated water** (`waterFrame`, `G06_water` / `G08_beach` `_f2/_f3`): ground chunks hold frame 1; a `HOOKS.town` item at base −1e9 redraws the visible river/sea vertices with frames 2/3 (silhouettes identical), skipping bridge/pier tiles and deep sea. The same item draws `WDEC` (reeds, lily pads, river rocks, driftwood, footprints) placed once by tile hash.
**Cave mouth**: the crystal-cave object swaps to `cave_mouth` (native size, bottom-centred in the old 56×44 box, set 20px into the cliff face).
Also from the pack: `wf_mist_*` puffs at waterfall feet, shells/starfish in `WDEC`, rock objects swap to `rock_1..4` boulders (16×16, same width and ground line), the cafe deck decal tiles `deck_1..4`, an open `gate_open` decal at the ranch entrance.
Not used (no matching thing in the game): `ramp_e/w/s` (diagonal ledge roads vs. straight road crossings), `picket_*`/`stonewall_*`/`hedge_*`, `gate_closed`, `arena_floor_*` (arena uses `arena_round`), `rivermouth_*` (the river ends in a beach lagoon).
**HD terrain pass** (`hdTerrain`, called from `paintChunk`): per-pixel curved river banks (wet sand, pebbles, foam, depth), bulging Voronoi rock cliffs with grass lips and contact shadows, east/west terrace slopes, smooth terrace light, curved sand/path ↔ grass borders (copies texture from the neighbouring tile). Cliffs come from a smoothed height contour (blurred `HGT` sampled with wobble → `LV` level map, column scan → face depth `CD`, side slopes `SD`), so terrace edges curve; collision stays tile-based. Bridges are timber deck decals built per crossing in `worldRebuild` (step 7b); `straight()` re-routes each water crossing as a straight run so the bridge lines up with the road. East/west terrace edges are solid rock side faces (`CLIFF=5`, SOL, no hedges); roads crossing them stay open as ramps. Stairs (`CLIFF` 2/4) are drawn in `paintChunk` after the decals. Works per world pixel (cost ≈ 10 ms per chunk); keep it cheap.
Ground tiles are separate: `TILE_ART` maps ground type → `tile_<name>` (+ `_edge_<nw|n|…|inner_se>` where it meets grass), drawn by `tileArt()` in `paintTile`; cliffs/stairs stay code-drawn.
Loaded over http(s) only. To add a set: copy PNGs to `game/assets/<set>/`, add `artReg` at the sprite's creation site, list it in the manifest.

### Save data
`snapshot()` / `restore(d)` (search `function snapshot`). **Any new field in `S` that must persist has to be added to both.**
`restore` deep-clones its input first (cloud data can be frozen). Saves go to `localStorage` and the artifact DB.

## Hard-won pitfalls (each one shipped a bug)
1. **Never call a `const`/`let` before its declaration line** at load time. A function that reads a `const` declared later
   crashes the whole script → blank green screen, no error box. Use `function` declarations for helpers that run during load.
2. **Duplicate `function name()` declarations silently override each other** (the later one wins everywhere). Happened with
   `makeNet` (badminton net became a beach volleyball net). Grep before naming. Duplicate `const` names fail to parse.
3. **Cloud save objects can be frozen.** Mutating them throws "Cannot assign to read only property". Copy before mutating.
4. **Absolute-positioned overlays on mobile:** heights must account for browser chrome (`100dvh`), scroll the body, keep action buttons in a fixed footer/header.
5. **HUD rows must wrap** (`flex-wrap`) — the left column is narrow on phones.
6. Placing objects: check the tile isn't a road/door path and isn't already `SOL`. Door of a dept house is at sprite x 47..65 (`hx*T+56`).
7. Interactable markers that float above the y-sorted world look wrong; anchor them on the ground via the `town`/`interior` hooks.
8. Drawing text: use `"Chakra Petch"` for readable UI text; `"Pixelify Sans"` only inside baked sprite signs.
9. Things that must be bright at night (neon, lamps, fireworks) belong in `HOOKS.top` plus a `LIGHTS` entry for the light pool.

## Systems map (where to look)
| System | Search for |
|---|---|
| Fishing (pond, lake, sea) | `fishing v2`, `FISHDB`, `rollFish` |
| Waste sorting | `waste sorting`, `sorting overlay` |
| Boss raid | `raid boss`, `BOSS_MAX` |
| Inventory / shops / sell decay | `items + inventory`, `shop UI`, `unitPrice` |
| Team fund & house upgrades (10 levels, floors 2–3, lift) | `team fund`, `UPG`, `makeCottage`, `liftMenu` |
| Knowledge tokens (2nd currency) | `knowledge tokens`, `KP_SRC` |
| Retention: mail, login calendar, season, level rewards, story | `retention:`, `STORY`, `seasonReward`, `LOGIN_REWARDS` |
| Personal office + plant care | `personal office`, `PLANTS` |
| Secret merchant | `secret traveling merchant`, `GYPSY_` |
| Fast travel | `Midway Shuttle`, `STOPS` |
| Audio | `sound + music`, `SFX`, `TRACKS` |
| Admin/test panel | `economy settings (admin)`, `debugAction` |
| Map layout editor (drag & drop, `layout.json`) | `map layout editor`, `LAY`, `layApply` |

## Runtime dependencies to replace when self-hosting
The game was built as a claude.ai artifact. Online features call the artifact runtime (`window.claude.use('user'|'db'|'room')`)
for login name, cloud save, shared player docs (team fund totals, raid damage, votes) and live presence.
When hosted inside the Midway portal these must be replaced by the Go API in `api/` (KID login, server-authoritative economy).
Without them the game runs offline with `localStorage` only.

## Working agreements
- Small, focused changes; run `npm run smoke` before committing.
- Keep everything pixel-art and in Thai. Mobile and desktop both matter (touch joystick + keyboard/mouse).
- When adding currency sources, add a daily cap in `CFG` — inflation control is a design goal.
- Prefer splitting the file into modules (Vite + TS) before large new features — see `docs/BACKLOG.md`.
