package tool

import (
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
)

type ArithmeticTool struct{}

func (t *ArithmeticTool) Name() string { return "eval_arithmetic" }
func (t *ArithmeticTool) Description() string {
	return "Evaluates an arithmetic expression using Go constant expression syntax. " +
		"Supports +, -, *, /, parentheses, and decimal numbers. " +
		"Examples: '(1+2)*3','10.0/3.0', '100-25*2'."
}
func (t *ArithmeticTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expression": map[string]any{
				"type":        "string",
				"description": "The arithmetic expression to evaluate, e.g. '(1+2)*3'.",
			},
		},
		"required": []string{"expression"},
	}
}
func (t *ArithmeticTool) Execute(args string) (string, error) {
	var p struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	fs := token.NewFileSet()
	tv, err := types.Eval(fs, nil, token.NoPos, p.Expression)
	if err != nil {
		return "", fmt.Errorf("cannot evaluate: %w", err)
	}
	return tv.Value.ExactString(), nil
}
func (t *ArithmeticTool) Approval() bool  { return false }
func (t *ArithmeticTool) Workspace() bool { return false }
