package repointel

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestImportGraph(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"src/app.ts": `import { util } from './util.js';            // .js names util.ts
import type { Config } from './config.js';   // type only: none
import { type Row, helper } from './rows';   // helper is a value
import { onlyType } from './types';          // used only as a type: none
import { unused } from './unused';           // not used: none
import * as db from './db/index.js';         // an index named
import { widget } from './widgets';          // a directory: its index
import './side-effect';                      // always kept
import React from 'react';                   // a package: none
export { reexported } from './re';           // kept
export type { T } from './re-type';          // type only: none
const lazy = () => import('./lazy');
const legacy = require('../lib/legacy');
let x: typeof db = db;
const c: onlyType = util(helper(widget));
function f(v: Config) { return v as unknown as Row; }
`,
		"src/util.ts": "export function util() {}\n", "src/config.ts": "", "src/rows.ts": "", "src/types.ts": "",
		"src/unused.ts": "", "src/db/index.ts": "", "src/widgets/index.ts": "", "src/side-effect.ts": "", "src/re.ts": "", "src/re-type.ts": "",
		"src/lazy.tsx": "", "lib/legacy.js": "",
		"src/view.jsx":   "import Button from './Button.jsx';\nexport const V = () => <Button />;\n",
		"src/Button.tsx": "", "src/self.ts": "import './self';\n",
	}
	for name, text := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(text), 0o644)
	}
	list, _ := Walk(dir)
	got := map[Edge]bool{}
	for _, e := range ImportGraph(dir, list) {
		got[e] = true
	}
	want := map[Edge]bool{
		{"src/app.ts", "src/util.ts"}: true, {"src/app.ts", "src/rows.ts"}: true, {"src/app.ts", "src/db/index.ts"}: true,
		{"src/app.ts", "src/side-effect.ts"}: true, {"src/app.ts", "src/re.ts"}: true, {"src/app.ts", "src/lazy.tsx"}: true,
		{"src/app.ts", "lib/legacy.js"}: true, {"src/view.jsx", "src/Button.tsx"}: true, {"src/app.ts", "src/widgets/index.ts"}: true,
	}
	if !reflect.DeepEqual(got, want) {
		for e := range got {
			if !want[e] {
				t.Errorf("extra edge %v", e)
			}
		}
		for e := range want {
			if !got[e] {
				t.Errorf("missing edge %v", e)
			}
		}
	}
}
