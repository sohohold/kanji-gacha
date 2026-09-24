// Renders the test corpus with the reference engine (@kurgm/kage-engine,
// pinned in package.json) and writes ../golden.json.gz, which the Go tests in
// internal/kage compare against. Node is needed only to regenerate the file:
//
//   cd internal/kage/testdata/gen && npm ci && node gen.mjs
//
// Glyph data taken from GlyphWiki (https://glyphwiki.org/) is free of
// restrictions; the stroke matrix follows the one in the reference engine's
// test/strokes.ts.
import { writeFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { Kage, Polygons } from "@kurgm/kage-engine";

const cases = [];

// ─── Single strokes ─────────────────────────────────────────────────────────
const POINT_COUNT = { 1: 2, 2: 3, 3: 3, 4: 3, 6: 4, 7: 4 };
const GEOMETRIES = {
	h: [20, 50, 80, 50, 140, 50, 180, 50],
	v: [50, 20, 50, 80, 50, 140, 50, 180],
	d: [30, 30, 70, 80, 120, 130, 170, 175],
	rl: [180, 150, 120, 120, 70, 80, 20, 30],
	// A slanted, shallow and a steep stroke, and one sitting on the baseline.
	s: [40, 60, 150, 75, 160, 150, 100, 190],
	t: [100, 20, 90, 120, 60, 160, 20, 185],
	b: [30, 170, 170, 196, 60, 196, 100, 199],
};
const HEADS = [0, 1, 2, 6, 7, 12, 22, 27, 32, 132, 102, 1000, 2000, 10000];
const TAILS = [0, 1, 2, 4, 5, 7, 8, 9, 13, 14, 15, 23, 24, 32, 313, 413, 100, 200, 300, 104, 500, 1004, 5000, 10005];
for (const [type, n] of Object.entries(POINT_COUNT)) {
	for (const [g, coords] of Object.entries(GEOMETRIES)) {
		const points = coords.slice(0, n * 2).join(":");
		for (const head of HEADS) {
			for (const tail of TAILS) {
				cases.push({ id: `stroke:${type}:${head}:${tail}:${g}`, data: `${type}:${head}:${tail}:${points}` });
			}
		}
	}
}
// Type 1 with an option in the first column, and malformed lines.
cases.push({ id: "odd:101", data: "101:0:0:20:50:180:50$1:0:0:100:20:100:180" });
cases.push({ id: "odd:short", data: "1:0:0:20:50$2:7:8:30:30:120" });
cases.push({ id: "odd:text", data: "1:0:0:20:x:180:50$1:0:0: 40 :60:180:60$$1:0:0:::180:90" });
cases.push({ id: "odd:frac", data: "1:0:0:20.7:50.2:180.9:50.5$1:0:0:100.5:20:100.5:180" });

// ─── Glyphs ─────────────────────────────────────────────────────────────────
// Glyph data from GlyphWiki, as quoted in the reference engine's tests.
const glyphwiki = {
	u9ebb: "1:0:2:40:37:143:37$4:22:5:143:37:12:169:170:169:175:171",
	"u9ebb-2": "99:0:0:0:0:200:200:u9ebb:0:0:0",
	"u2010f-jv": "99:0:0:0:0:200:200:u4e86-j$0:99:2:0:0:200:200",
	"u4e86-j": "1:0:2:26:32:158:32$2:22:7:158:32:133:54:100:78$1:0:4:100:74:100:181",
	u6f22: "99:150:0:9:12:73:200:u6c35-07:0:-10:50$99:0:0:54:10:190:199:u26c29-07",
	"u6c35-07": "2:7:8:42:12:99:23:124:35$2:7:8:20:62:75:71:97:85$2:7:8:12:123:90:151:81:188$2:2:7:63:144:109:118:188:51",
	"u26c29-07": "1:0:0:18:29:187:29$1:0:0:73:10:73:48$1:0:0:132:10:132:48$1:12:13:44:59:44:87$1:2:2:44:59:163:59$1:22:23:163:59:163:87$1:2:2:44:87:163:87$1:0:0:32:116:176:116$1:0:0:21:137:190:137$7:32:7:102:59:102:123:102:176:10:190$2:7:0:105:137:126:169:181:182",
	"twe_sandbox@81": [
		"1:12:13:25:28:24:95", "1:2:2:25:28:81:28", "1:22:23:81:28:80:95", "1:32:32:53:28:52:95",
		"1:0:0:12:61:100:61", "1:2:2:24:95:80:95", "1:0:4:108:21:110:93", "1:0:0:165:23:165:46",
		"1:0:413:142:15:132:63", "1:2:0:132:63:175:57", "1:0:313:139:79:139:99", "1:2:2:139:99:160:99",
		"1:0:24:160:66:160:99", "2:0:7:30:130:27:167:11:187", "2:0:7:27:184:47:178:65:165",
		"2:0:5:37:110:44:133:57:152", "2:7:4:119:104:135:117:135:134", "2:32:7:84:106:77:124:62:142",
		"1:0:2:59:106:102:106", "3:22:5:102:106:96:165:122:165", "3:0:0:78:140:76:180:117:180",
		"4:0:5:164:109:131:179:177:179", "6:7:8:132:106:155:112:146:145:171:139",
		"7:0:7:184:14:184:88:184:146:166:171",
	].join("$"),
	stretch: "99:150:0:10:10:190:190:inner:0:-10:50",
	inner: "1:0:2:20:40:180:40$1:12:13:40:40:40:160$2:7:8:60:60:120:100:160:160",
	missing: "99:0:0:0:0:200:200:not-registered$1:0:0:20:100:180:100",
};
for (const name of ["u9ebb-2", "u2010f-jv", "u6f22", "twe_sandbox@81", "stretch", "missing"]) {
	cases.push({ id: `glyph:${name}`, data: glyphwiki[name] });
}
for (const [op, n] of [["97:0", "t97"], ["98:0", "t98"], ["99:1", "r90"], ["99:2", "r180"], ["99:3", "r270"]]) {
	cases.push({ id: `transform:${n}`, data: `1:0:2:20:40:180:40$1:12:13:40:40:40:160$0:${op}:0:0:200:200` });
}

// Components placed and stretched the way the generator places them: each
// part mapped into its own rectangle, drawn together so the engine adjusts
// strokes of one part against those of the others.
const parts = ["u6c35-07", "u26c29-07", "u9ebb", "u4e86-j", "twe_sandbox@81", "inner"];
const rects = [
	[0, 0, 200, 200], [0, 0, 70, 200], [60, 0, 200, 200], [0, 0, 200, 90], [0, 100, 200, 200],
	[20, 30, 180, 170], [-12, 5, 230, 190], [45, 45, 155, 155], [0, 0, 120, 60], [150, 10, 190, 195],
];
for (const [i, a] of parts.entries()) {
	for (const [j, b] of parts.entries()) {
		const [ra, rb] = [rects[(i + j) % rects.length], rects[(i * 3 + j + 1) % rects.length]];
		cases.push({
			id: `compose:${a}+${b}`,
			data: `99:0:0:${ra.join(":")}:${a}$99:0:0:${rb.join(":")}:${b}`,
		});
	}
}
cases.push({ id: "compose:side", data: "99:0:0:0:0:200:200:u6c35-07$99:0:0:0:0:200:200:u26c29-07" });
cases.push({ id: "compose:nested", data: "99:0:0:0:0:120:200:u9ebb-2$99:0:0:80:0:200:200:u6f22" });
cases.push({ id: "compose:stretch", data: "99:130:0:0:0:200:200:u26c29-07:0:-20:40" });

// Strokes crowded so every adjustment (tate, uroko, uroko2, mage, kakato,
// hane, kirikuchi) changes the shape.
cases.push({ id: "adjust:tate", data: "1:0:0:60:20:60:180$1:0:0:70:30:70:170$1:0:0:82:20:82:180$3:0:0:90:20:90:150:170:150$7:0:7:98:20:98:100:98:150:60:190" });
cases.push({ id: "adjust:uroko", data: "1:0:0:20:40:180:40$1:0:0:20:60:180:60$1:0:0:20:80:180:80$1:0:0:150:20:150:120$1:0:0:20:100:40:100$1:0:0:20:130:180:140" });
cases.push({ id: "adjust:mage", data: "3:0:5:40:20:40:100:180:100$1:0:0:40:110:180:110$3:0:0:60:40:60:130:170:130$1:0:0:20:140:180:140" });
cases.push({ id: "adjust:kakato", data: "1:0:13:40:20:40:150$1:0:0:20:160:180:160$1:0:23:160:20:160:180$1:0:0:140:190:180:190$1:0:13:100:20:100:190" });
cases.push({ id: "adjust:hane", data: "1:0:4:100:20:100:180$1:0:0:80:30:80:190$2:0:4:150:20:150:100:140:180$6:0:4:20:20:30:80:40:120:50:170$1:0:0:120:100:120:190" });
cases.push({ id: "adjust:kirikuchi", data: "1:0:0:20:40:180:40$2:32:7:100:40:80:100:20:160$2:32:7:190:40:170:100:120:160" });

// ─── Render ─────────────────────────────────────────────────────────────────
const round = (v) => Math.round(v * 10) / 10;
function render(data) {
	const kage = new Kage();
	for (const [k, v] of Object.entries(glyphwiki)) {
		kage.kBuhin.push(k, v);
	}
	const polygons = new Polygons();
	kage.makeGlyph2(polygons, data);
	const perStroke = kage.makeGlyph3(data).map((p) => p.array.length);
	return {
		polygons: polygons.array.map((p) => p.array.flatMap(({ x, y }) => [round(x), round(y)])),
		perStroke,
	};
}
for (const c of cases) {
	Object.assign(c, render(c.data));
}
const json = JSON.stringify({ engine: "@kurgm/kage-engine 0.7.0", buhin: glyphwiki, cases });
writeFileSync(new URL("../golden.json.gz", import.meta.url), gzipSync(json, { level: 9 }));
console.log(`${cases.length} cases, ${json.length} bytes`);
