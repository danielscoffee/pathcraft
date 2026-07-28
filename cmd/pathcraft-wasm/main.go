//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/danielscoffee/pathcraft/internal/wasmapi"
)

const invalidStringArgument = `{"ok":false,"error":"expected one string argument"}`

var callbacks []js.Func

func main() {
	api := wasmapi.New()
	bridge := js.Global().Get("Object").New()
	bindString(bridge, "loadOSM", api.LoadOSM)
	bindString(bridge, "route", api.Route)
	bind(bridge, "stats", func([]js.Value) any { return api.Stats() })
	js.Global().Set("pathcraftWasm", bridge)
	select {}
}

func bindString(target js.Value, name string, call func(string) string) {
	bind(target, name, func(args []js.Value) any {
		if len(args) != 1 || args[0].Type() != js.TypeString {
			return invalidStringArgument
		}
		return call(args[0].String())
	})
}

func bind(target js.Value, name string, call func([]js.Value) any) {
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any { return call(args) })
	callbacks = append(callbacks, callback)
	target.Set(name, callback)
}
