package bytecode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"

	"github.com/vasileknik76/tql/syntax"
)

const (
	// Magic and Version identify a portable bytecode image.
	Magic         = "TQLC"
	FormatVersion = 1
	headerSize    = 24

	sectionMeta      = 1
	sectionParam     = 2
	sectionConst     = 3
	sectionSlot      = 4
	sectionPath      = 5
	sectionTarget    = 6
	sectionTargetSet = 7
	sectionArgSet    = 8
	sectionScope     = 9
	sectionLoop      = 10
	sectionCode      = 11
	sectionMatch     = 12
)

// Encode returns a deterministic, versioned bytecode image.
func Encode(p *Program) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("bytecode: nil program")
	}
	if err := Validate(p); err != nil {
		return nil, fmt.Errorf("invalid bytecode: %w", err)
	}
	var payload bytes.Buffer
	if err := writeSection(&payload, sectionMeta, func(w *writer) error {
		w.u32(FormatVersion)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionParam, func(w *writer) error {
		w.u32(uint32(len(p.Params)))
		for _, param := range p.Params {
			w.string(param.Name)
			if param.HasDefault {
				w.u8(1)
				if err := writeValue(w, param.Default, 0); err != nil {
					return err
				}
			} else {
				w.u8(0)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionConst, func(w *writer) error {
		w.u32(uint32(len(p.Consts)))
		for _, c := range p.Consts {
			switch {
			case c.Texts != nil:
				w.u8(2)
				w.u32(uint32(len(c.Texts)))
				for _, text := range c.Texts {
					w.string(text)
				}
			case c.TextSet || c.Text != "":
				w.u8(1)
				w.string(c.Text)
			default:
				w.u8(0)
				if err := writeValue(w, c.Value, 0); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionSlot, func(w *writer) error {
		w.u32(uint32(len(p.Slots)))
		for _, slot := range p.Slots {
			w.string(slot)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionPath, func(w *writer) error {
		w.u32(uint32(len(p.Paths)))
		for _, path := range p.Paths {
			w.u32(uint32(len(path.Steps)))
			for _, step := range path.Steps {
				w.u8(uint8(step.Kind))
				switch step.Kind {
				case StepKey:
					w.string(step.Key)
				case StepIndex:
					w.i32(int32(step.Index))
				case StepSlice:
					w.u8(boolByte(step.HasFrom))
					if step.HasFrom {
						w.i32(int32(step.From))
					}
					w.u8(boolByte(step.HasTo))
					if step.HasTo {
						w.i32(int32(step.To))
					}
				case StepFilter, StepDynamic, StepMatch:
					w.i32(step.Code)
				case StepWildcard:
				default:
					return fmt.Errorf("unknown path step %d", step.Kind)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionTarget, func(w *writer) error {
		w.u32(uint32(len(p.Targets)))
		for _, target := range p.Targets {
			w.u8(uint8(target.Kind))
			w.i32(target.Base)
			w.i32(target.Path)
			w.u8(boolByte(target.Root))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionTargetSet, func(w *writer) error {
		w.u32(uint32(len(p.TargetSets)))
		for _, set := range p.TargetSets {
			w.u32(uint32(len(set)))
			for _, code := range set {
				w.i32(code)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionArgSet, func(w *writer) error {
		w.u32(uint32(len(p.ArgSets)))
		for _, set := range p.ArgSets {
			w.u32(uint32(len(set)))
			for _, reg := range set {
				w.i32(reg)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionScope, func(w *writer) error {
		w.u32(uint32(len(p.Scopes)))
		for _, scope := range p.Scopes {
			w.u32(uint32(len(scope.Slots)))
			for _, slot := range scope.Slots {
				w.i32(slot)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionLoop, func(w *writer) error {
		w.u32(uint32(len(p.Loops)))
		for _, loop := range p.Loops {
			w.u8(uint8(loop.Kind))
			w.i32(loop.Continue)
			w.i32(loop.Break)
			w.i32(loop.Scope)
			w.i32(loop.VarSlot)
			w.u8(boolByte(loop.ReadOnly))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionCode, func(w *writer) error {
		w.u32(uint32(len(p.Codes)))
		for _, code := range p.Codes {
			w.string(code.Name)
			w.u32(uint32(code.Registers))
			w.u32(uint32(len(code.Ops)))
			for _, op := range code.Ops {
				w.u8(uint8(op.Code))
				w.i32(op.A)
				w.i32(op.B)
				w.i32(op.C)
				w.i32(op.D)
				w.i32(op.Stmt)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := writeSection(&payload, sectionMatch, func(w *writer) error {
		w.u32(uint32(len(p.Matches)))
		for _, match := range p.Matches {
			w.i32(match.KeyConst)
			w.i32(match.Index)
			w.u8(boolByte(match.IsIndex))
			w.i32(match.OpConst)
			w.i32(match.ValueConst)
			w.u8(boolByte(match.HasValue))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return frame(payload.Bytes())
}

func writeSection(out *bytes.Buffer, id uint16, fn func(*writer) error) error {
	var body writer
	if err := fn(&body); err != nil {
		return err
	}
	var head [8]byte
	binary.LittleEndian.PutUint16(head[0:2], id)
	binary.LittleEndian.PutUint16(head[2:4], 0)
	binary.LittleEndian.PutUint32(head[4:8], uint32(len(body.buf)))
	out.Write(head[:])
	out.Write(body.buf)
	return nil
}

func frame(payload []byte) ([]byte, error) {
	if len(payload) > maxImageSize-headerSize {
		return nil, fmt.Errorf("bytecode image exceeds %d bytes", maxImageSize)
	}
	out := make([]byte, headerSize+len(payload))
	copy(out[0:4], Magic)
	binary.LittleEndian.PutUint16(out[4:6], FormatVersion)
	binary.LittleEndian.PutUint16(out[6:8], 0)
	binary.LittleEndian.PutUint32(out[8:12], headerSize)
	binary.LittleEndian.PutUint32(out[12:16], uint32(len(payload)))
	binary.LittleEndian.PutUint32(out[16:20], crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli)))
	binary.LittleEndian.PutUint32(out[20:24], 0)
	copy(out[headerSize:], payload)
	return out, nil
}

type writer struct {
	buf []byte
}

func (w *writer) u8(v uint8) { w.buf = append(w.buf, v) }
func (w *writer) u32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.buf = append(w.buf, b[:]...)
}
func (w *writer) i32(v int32) { w.u32(uint32(v)) }
func (w *writer) string(s string) {
	w.u32(uint32(len(s)))
	w.buf = append(w.buf, s...)
}

func boolByte(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func writeValue(w *writer, v any, depth int) error {
	if depth > maxValueDepth {
		return fmt.Errorf("value nesting exceeds %d levels", maxValueDepth)
	}
	normalized, err := syntax.NormalizeValue(v)
	if err != nil {
		return err
	}
	switch x := normalized.(type) {
	case nil:
		w.u8(0)
	case bool:
		if x {
			w.u8(2)
		} else {
			w.u8(1)
		}
	case syntax.Number:
		w.u8(3)
		w.string(string(x))
	case string:
		w.u8(4)
		w.string(x)
	case syntax.Temporal:
		w.u8(5)
		w.string(string(x))
	case []any:
		w.u8(6)
		w.u32(uint32(len(x)))
		for _, value := range x {
			if err := writeValue(w, value, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		w.u8(7)
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		w.u32(uint32(len(keys)))
		for _, key := range keys {
			w.string(key)
			if err := writeValue(w, x[key], depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported value %T", v)
	}
	return nil
}
