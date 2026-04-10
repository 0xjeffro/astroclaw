package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

// makeArgs is a test helper that wraps an expression string into the JSON
// format that OpenAI would send as tool_call arguments:
// {"expression": "1+2"}
// This keeps individual test cases focused on the expression itself rather
// than JSON boilerplate.
func makeArgs(expr string) string {
	b, _ := json.Marshal(map[string]string{"expression": expr})
	return string(b)
}

// TestEvalArithmetic_BasicOps verifies that the four basic arithmetic
// operators produce correct results. These are the most common expressions
// the model will generate, so they must work reliably.
func TestEvalArithmetic_BasicOps(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"1+2", "3"},
		{"10-3", "7"},
		{"4*5", "20"},
		{"10.0/4.0", "5/2"},
	}
	for _, tt := range tests {
		got, err := EvalArithmetic.Run(makeArgs(tt.expr))
		if err != nil {
			t.Errorf("Run(%q): unexpected error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Run(%q): got %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestEvalArithmetic_Parentheses verifies that parentheses correctly
// override operator precedence. Without parentheses, 1+2*3 = 7; with
// them, (1+2)*3 = 9. If this test fails, types.Eval is not respecting
// grouping — which would be a Go stdlib bug, but we pin it down anyway
// because our tool's description promises parentheses support.
func TestEvalArithmetic_Parentheses(t *testing.T) {
	got, err := EvalArithmetic.Run(makeArgs("(1+2)*3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "9" {
		t.Errorf("got %q, want %q", got, "9")
	}
}

// TestEvalArithmetic_FloatDivision verifies that float division returns
// an exact rational fraction. types.Eval uses Go's arbitrary-precision
// constant arithmetic: 10.0/3.0 produces the exact fraction "10/3",
// not a truncated decimal. The model receives this fraction as-is and
// can interpret it correctly in its response to the user.
func TestEvalArithmetic_FloatDivision(t *testing.T) {
	got, err := EvalArithmetic.Run(makeArgs("10.0/3.0"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10/3" {
		t.Errorf("got %q, want %q (exact fraction)", got, "10/3")
	}
}

// TestEvalArithmetic_IntegerDivision documents Go's integer division
// semantics: 10/3 truncates to 3, not 3.333 or "10/3". This is standard
// Go behavior (integer operands produce integer results). We pin this
// explicitly so that if types.Eval behavior changes across Go versions,
// we notice. The model can work around this by using float operands
// (10.0/3.0) when it needs a precise result.
func TestEvalArithmetic_IntegerDivision(t *testing.T) {
	got, err := EvalArithmetic.Run(makeArgs("10/3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "3" {
		t.Errorf("got %q, want %q (integer truncation)", got, "3")
	}
}

// TestEvalArithmetic_InvalidExpression verifies that a syntactically
// invalid expression (not valid Go constant syntax) returns an error
// rather than panicking or returning garbage. The error will be wrapped
// with "cannot evaluate:" prefix from our Run function. In practice,
// this error string gets fed back to the model as a tool result, and
// the model will either retry with a corrected expression or fall back
// to answering without the tool.
func TestEvalArithmetic_InvalidExpression(t *testing.T) {
	_, err := EvalArithmetic.Run(makeArgs("abc"))
	if err == nil {
		t.Fatal("expected error for invalid expression, got nil")
	}
	if !strings.Contains(err.Error(), "cannot evaluate") {
		t.Errorf("error %q should contain 'cannot evaluate'", err.Error())
	}
}

// TestEvalArithmetic_BadJSON verifies that malformed JSON arguments
// (i.e. the arguments string from OpenAI is corrupted or not valid JSON)
// return an error rather than panicking. This is the first line of defense
// in Run: json.Unmarshal failing means something is seriously wrong with
// the tool_call payload, and we surface it cleanly with a "bad args" prefix.
func TestEvalArithmetic_BadJSON(t *testing.T) {
	_, err := EvalArithmetic.Run("not json")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "bad args") {
		t.Errorf("error %q should contain 'bad args'", err.Error())
	}
}

// TestEvalArithmetic_DivisionByZero verifies that dividing by zero returns
// an error rather than panicking or returning Inf. In Go constant
// evaluation, integer division by zero is a compile-time error, so
// types.Eval will reject it. The error gets fed back to the model,
// which can then explain to the user why the expression is invalid.
func TestEvalArithmetic_DivisionByZero(t *testing.T) {
	_, err := EvalArithmetic.Run(makeArgs("1/0"))
	if err == nil {
		t.Fatal("expected error for division by zero, got nil")
	}
}
