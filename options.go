package tql

import (
	"fmt"
	"sort"

	"github.com/vasileknik76/tql/internal/query"
	"github.com/vasileknik76/tql/syntax"
)

// EvalOption configures parsing or execution in Eval.
type EvalOption interface{ applyEval(*evalOptions) error }

// ParseOption configures a document at parse time. It can also be used in Eval.
type ParseOption interface {
	EvalOption
	applyParse(*parseOptions) error
}

// ExecOption configures one execution. It can also be used in Eval.
type ExecOption interface {
	EvalOption
	applyExec(*execOptions) error
}

type execOptions struct {
	params    map[string]any
	paramsSet bool
}
type evalOptions struct {
	parse []ParseOption
	exec  execOptions
}
type paramsOption struct {
	params map[string]any
	err    error
}

// WithParams captures a normalized, deep snapshot of named input parameters.
// Names omit the leading $. Invalid values are reported when executing.
// An execution accepts at most one WithParams option. Options can be reused
// concurrently; changing the original map after this call has no effect.
func WithParams(params map[string]any) ExecOption {
	keys := make([]string, 0, len(params))
	for name := range params {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	values := make(map[string]any, len(params))
	for _, name := range keys {
		if !query.ValidVariableName(name) {
			return paramsOption{err: fmt.Errorf("invalid parameter name %q", name)}
		}
		value, err := syntax.NormalizeValue(params[name])
		if err != nil {
			return paramsOption{err: fmt.Errorf("parameter $%s: %w", name, err)}
		}
		values[name] = value
	}
	return paramsOption{params: values}
}
func (p paramsOption) applyExec(o *execOptions) error {
	if o.paramsSet {
		return fmt.Errorf("WithParams may only be used once per execution")
	}
	if p.err != nil {
		return p.err
	}
	o.params, o.paramsSet = p.params, true
	return nil
}
func (p paramsOption) applyEval(o *evalOptions) error { return p.applyExec(&o.exec) }

func executionOptions(options []ExecOption) (execOptions, error) {
	var o execOptions
	for _, option := range options {
		if option == nil {
			return o, fmt.Errorf("nil execution option")
		}
		if err := option.applyExec(&o); err != nil {
			return o, err
		}
	}
	return o, nil
}

func (o execOptions) bindings(p *Program) (map[string]*binding, error) {
	if len(o.params) > 0 {
		declared := make(map[string]bool, len(p.program.Parameters))
		for _, param := range p.program.Parameters {
			declared[param.Name] = true
		}
		unknown := make([]string, 0)
		for name := range o.params {
			if !declared[name] {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return nil, fmt.Errorf("unknown parameter %q", unknown[0])
		}
	}
	values := make(map[string]*binding, len(p.program.Parameters))
	for _, param := range p.program.Parameters {
		value, supplied := o.params[param.Name]
		if !supplied {
			if !param.HasDefault {
				return nil, fmt.Errorf("missing required parameter $%s", param.Name)
			}
			value = param.Default
		}
		values[param.Name] = &binding{value: query.Node(value), readonly: param.Name}
	}
	return values, nil
}
