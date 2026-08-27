package apisurface

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSSymbolsNamedDeclarations(t *testing.T) {
	src := `export function fetchUser(id) {}
export async function fetchAll() {}
export class Widget {}
export const MAX = 10;
let hidden = 1;
export interface Options {}
export type Alias = string;
`
	syms := jsSymbols([]byte(src))
	require.ElementsMatch(t, []string{"fetchUser", "fetchAll", "Widget", "MAX", "Options", "Alias"}, names(syms))
}

func TestJSSymbolsDefaultExport(t *testing.T) {
	src := `export default function main() {}
`
	require.ElementsMatch(t, []string{"main"}, names(jsSymbols([]byte(src))))

	src2 := `function helper() {}
export default helper;
`
	require.ElementsMatch(t, []string{"helper"}, names(jsSymbols([]byte(src2))))
}

func TestJSSymbolsExportList(t *testing.T) {
	src := `export { fetchUser, fetchAll as fetchEverything };
`
	require.ElementsMatch(t, []string{"fetchUser", "fetchEverything"}, names(jsSymbols([]byte(src))))
}

func TestJSSymbolsModuleExports(t *testing.T) {
	src := `module.exports.fetchUser = function () {};
module.exports = { fetchAll, save: doSave };
`
	require.ElementsMatch(t, []string{"fetchUser", "fetchAll", "save"}, names(jsSymbols([]byte(src))))
}

func TestJSSymbolsEmpty(t *testing.T) {
	require.Empty(t, jsSymbols(nil))
}
