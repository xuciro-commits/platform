//go:build js && wasm

package main

import (
	"encoding/json"
	ruleexpr "platformserver/expr"
	"syscall/js"
)

func main() {
	bind := func(name string, run func([]byte) ([]byte, error)) {
		call := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) != 1 || args[0].Type() != js.TypeString {
				return `{"error":{"code":"request","at":"$","message":"one encoded rule request required"}}`
			}
			result, err := run([]byte(args[0].String()))
			if err != nil {
				issue, ok := err.(*ruleexpr.Issue)
				if !ok {
					issue = &ruleexpr.Issue{Code: "request", At: "$", Message: err.Error()}
				}
				raw, _ := json.Marshal(map[string]any{"error": issue})
				return string(raw)
			}
			return string(result)
		})
		js.Global().Set(name, call)
	}
	bind("platformRuleEval", ruleexpr.EvalJSON)
	bind("platformRuleCompile", ruleexpr.CompileJSON)
	bind("platformRulePage", ruleexpr.LowerPageJSON)
	js.Global().Set("platformRuleVersion", ruleexpr.Version)
	select {}
}
