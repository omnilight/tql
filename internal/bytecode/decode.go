package bytecode

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"unicode/utf8"

	"github.com/vasileknik76/tql/syntax"
)

// Decode validates and loads a portable bytecode image.
func Decode(data []byte) (*Program, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("bytecode image is too short")
	}
	if string(data[0:4]) != Magic {
		return nil, fmt.Errorf("not a TQL bytecode image")
	}
	if version := binary.LittleEndian.Uint16(data[4:6]); version != FormatVersion {
		return nil, fmt.Errorf("bytecode format v%d is not supported by this build (supports v%d); recompile the program", version, FormatVersion)
	}
	if flags := binary.LittleEndian.Uint16(data[6:8]); flags != 0 {
		return nil, fmt.Errorf("unsupported bytecode flags %#x", flags)
	}
	if size := binary.LittleEndian.Uint32(data[8:12]); size != headerSize {
		return nil, fmt.Errorf("unsupported bytecode header size %d", size)
	}
	payloadLen := binary.LittleEndian.Uint32(data[12:16])
	if uint64(headerSize)+uint64(payloadLen) != uint64(len(data)) {
		return nil, fmt.Errorf("bytecode payload length mismatch")
	}
	if len(data) > maxImageSize {
		return nil, fmt.Errorf("bytecode image exceeds %d bytes", maxImageSize)
	}
	payload := data[headerSize:]
	want := binary.LittleEndian.Uint32(data[16:20])
	if got := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli)); got != want {
		return nil, fmt.Errorf("bytecode checksum mismatch")
	}
	if reserved := binary.LittleEndian.Uint32(data[20:24]); reserved != 0 {
		return nil, fmt.Errorf("unsupported bytecode header extension")
	}
	sections, err := readSections(payload)
	if err != nil {
		return nil, err
	}
	p := &Program{}
	required := []uint16{sectionMeta, sectionParam, sectionConst, sectionSlot, sectionPath, sectionTarget, sectionTargetSet, sectionArgSet, sectionScope, sectionLoop, sectionMatch, sectionCode}
	for _, id := range required {
		if _, ok := sections[id]; !ok {
			return nil, fmt.Errorf("bytecode section %d is missing", id)
		}
	}
	if err := decodeMeta(sections[sectionMeta]); err != nil {
		return nil, err
	}
	if p.Params, err = decodeParams(sections[sectionParam]); err != nil {
		return nil, err
	}
	if p.Consts, err = decodeConsts(sections[sectionConst]); err != nil {
		return nil, err
	}
	if p.Slots, err = decodeStrings(sections[sectionSlot]); err != nil {
		return nil, err
	}
	if p.Paths, err = decodePaths(sections[sectionPath]); err != nil {
		return nil, err
	}
	if p.Targets, err = decodeTargets(sections[sectionTarget]); err != nil {
		return nil, err
	}
	if p.TargetSets, err = decodeSets(sections[sectionTargetSet]); err != nil {
		return nil, err
	}
	if p.ArgSets, err = decodeSets(sections[sectionArgSet]); err != nil {
		return nil, err
	}
	if p.Scopes, err = decodeScopes(sections[sectionScope]); err != nil {
		return nil, err
	}
	if p.Loops, err = decodeLoops(sections[sectionLoop]); err != nil {
		return nil, err
	}
	if p.Matches, err = decodeMatches(sections[sectionMatch]); err != nil {
		return nil, err
	}
	if p.Codes, err = decodeCodes(sections[sectionCode]); err != nil {
		return nil, err
	}
	p.Mutates = DeriveMutates(p)
	if err := Validate(p); err != nil {
		return nil, fmt.Errorf("invalid bytecode: %w", err)
	}
	return p, nil
}

func readSections(payload []byte) (map[uint16][]byte, error) {
	sections := map[uint16][]byte{}
	var last uint16
	for len(payload) > 0 {
		if len(payload) < 8 {
			return nil, fmt.Errorf("truncated bytecode section header")
		}
		id := binary.LittleEndian.Uint16(payload[0:2])
		flags := binary.LittleEndian.Uint16(payload[2:4])
		size := binary.LittleEndian.Uint32(payload[4:8])
		payload = payload[8:]
		if flags != 0 {
			return nil, fmt.Errorf("unsupported bytecode section flags %#x", flags)
		}
		if id <= last {
			return nil, fmt.Errorf("bytecode sections are out of order or duplicated")
		}
		if id > sectionMatch {
			return nil, fmt.Errorf("unknown bytecode section %d", id)
		}
		if uint64(size) > uint64(len(payload)) {
			return nil, fmt.Errorf("truncated bytecode section %d", id)
		}
		if _, exists := sections[id]; exists {
			return nil, fmt.Errorf("duplicate bytecode section %d", id)
		}
		sections[id] = payload[:size]
		payload = payload[size:]
		last = id
	}
	return sections, nil
}

type reader struct {
	buf []byte
	off int
	err error
}

func (r *reader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, args...)
	}
}
func (r *reader) u8() uint8 {
	if r.err != nil {
		return 0
	}
	if r.off >= len(r.buf) {
		r.fail("truncated byte at %d", r.off)
		return 0
	}
	v := r.buf[r.off]
	r.off++
	return v
}
func (r *reader) u32() uint32 {
	if r.err != nil {
		return 0
	}
	if r.off+4 > len(r.buf) {
		r.fail("truncated u32 at %d", r.off)
		return 0
	}
	v := binary.LittleEndian.Uint32(r.buf[r.off:])
	r.off += 4
	return v
}
func (r *reader) i32() int32 { return int32(r.u32()) }
func (r *reader) str() string {
	if r.err != nil {
		return ""
	}
	n := r.u32()
	if uint64(n) > uint64(len(r.buf)-r.off) || n > maxNameLength {
		r.fail("invalid string length %d", n)
		return ""
	}
	s := string(r.buf[r.off : r.off+int(n)])
	r.off += int(n)
	if !utf8.ValidString(s) {
		r.fail("invalid UTF-8 string")
	}
	return s
}
func (r *reader) count(limit int) int {
	n := r.u32()
	if uint64(n) > uint64(limit) || n > 1<<30 {
		r.fail("invalid collection length %d", n)
		return 0
	}
	return int(n)
}
func (r *reader) done() error {
	if r.err != nil {
		return r.err
	}
	if r.off != len(r.buf) {
		return fmt.Errorf("trailing bytes in section")
	}
	return nil
}

func decodeMeta(body []byte) error {
	r := reader{buf: body}
	version := r.u32()
	if r.err != nil {
		return r.err
	}
	if version != FormatVersion {
		return fmt.Errorf("bytecode opcode table version %d is not supported", version)
	}
	return r.done()
}

func decodeParams(body []byte) ([]Param, error) {
	r := reader{buf: body}
	count := r.count(maxSets)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]Param, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		param := Param{Name: r.str()}
		has := r.u8()
		if has > 1 {
			r.fail("invalid parameter default flag")
			break
		}
		param.HasDefault = has == 1
		if param.HasDefault {
			value, err := readValue(&r, 0)
			if err != nil {
				r.fail("%v", err)
				break
			}
			param.Default = value
		}
		out = append(out, param)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeConsts(body []byte) ([]Const, error) {
	r := reader{buf: body}
	count := r.count(maxConstants)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]Const, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		kind := r.u8()
		c := Const{}
		switch kind {
		case 0:
			value, err := readValue(&r, 0)
			if err != nil {
				r.fail("%v", err)
				break
			}
			c.Value = value
		case 1:
			c.Text = r.str()
			c.TextSet = true
		case 2:
			n := r.count(maxConstants)
			c.Texts = make([]string, 0, n)
			for j := 0; j < n && r.err == nil; j++ {
				c.Texts = append(c.Texts, r.str())
			}
		default:
			r.fail("unknown constant kind %d", kind)
		}
		out = append(out, c)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeStrings(body []byte) ([]string, error) {
	r := reader{buf: body}
	count := r.count(maxSlots)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]string, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		out = append(out, r.str())
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodePaths(body []byte) ([]PathPlan, error) {
	r := reader{buf: body}
	count := r.count(maxPaths)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]PathPlan, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		steps := r.count(maxPathSteps)
		path := PathPlan{}
		if steps > 0 {
			path.Steps = make([]Step, 0, steps)
		}
		for j := 0; j < steps && r.err == nil; j++ {
			step := Step{Kind: StepKind(r.u8())}
			if step.Kind > StepMatch {
				r.fail("unknown path step kind %d", step.Kind)
				break
			}
			switch step.Kind {
			case StepKey:
				step.Key = r.str()
			case StepIndex:
				step.Index = int(r.i32())
			case StepSlice:
				hasFrom := r.u8()
				if hasFrom > 1 {
					r.fail("invalid slice flag")
					break
				}
				step.HasFrom = hasFrom == 1
				if step.HasFrom {
					step.From = int(r.i32())
				}
				hasTo := r.u8()
				if hasTo > 1 {
					r.fail("invalid slice flag")
					break
				}
				step.HasTo = hasTo == 1
				if step.HasTo {
					step.To = int(r.i32())
				}
			case StepFilter, StepDynamic, StepMatch:
				step.Code = r.i32()
			case StepWildcard:
			}
			path.Steps = append(path.Steps, step)
		}
		out = append(out, path)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeTargets(body []byte) ([]TargetPlan, error) {
	r := reader{buf: body}
	count := r.count(maxTargets)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]TargetPlan, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		kind := TargetKind(r.u8())
		if kind != TargetPath && kind != TargetExpr {
			r.fail("unknown target kind %d", kind)
			break
		}
		target := TargetPlan{Kind: kind, Base: r.i32(), Path: r.i32()}
		root := r.u8()
		if root > 1 {
			r.fail("invalid target root flag")
			break
		}
		target.Root = root == 1
		out = append(out, target)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeSets(body []byte) ([][]int32, error) {
	r := reader{buf: body}
	count := r.count(maxSets)
	if count == 0 {
		return nil, r.done()
	}
	out := make([][]int32, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		n := r.count(maxInstructions)
		set := make([]int32, 0, n)
		for j := 0; j < n && r.err == nil; j++ {
			set = append(set, r.i32())
		}
		out = append(out, set)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeScopes(body []byte) ([]Scope, error) {
	r := reader{buf: body}
	count := r.count(maxSets)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]Scope, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		n := r.count(maxSlots)
		scope := Scope{}
		if n > 0 {
			scope.Slots = make([]int32, 0, n)
		}
		for j := 0; j < n && r.err == nil; j++ {
			scope.Slots = append(scope.Slots, r.i32())
		}
		out = append(out, scope)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeLoops(body []byte) ([]Loop, error) {
	r := reader{buf: body}
	count := r.count(maxSets)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]Loop, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		loop := Loop{
			Kind:     LoopKind(r.u8()),
			Continue: r.i32(),
			Break:    r.i32(),
			Scope:    r.i32(),
			VarSlot:  r.i32(),
			ReadOnly: r.u8() == 1,
		}
		out = append(out, loop)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeMatches(body []byte) ([]MatchPlan, error) {
	r := reader{buf: body}
	count := r.count(maxSets)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]MatchPlan, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		match := MatchPlan{
			KeyConst: r.i32(),
			Index:    r.i32(),
		}
		isIndex := r.u8()
		match.OpConst = r.i32()
		match.ValueConst = r.i32()
		hasValue := r.u8()
		if isIndex > 1 || hasValue > 1 {
			r.fail("invalid match flag")
			break
		}
		match.IsIndex = isIndex == 1
		match.HasValue = hasValue == 1
		out = append(out, match)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeCodes(body []byte) ([]*Code, error) {
	r := reader{buf: body}
	count := r.count(maxCodeObjects)
	if count == 0 {
		return nil, r.done()
	}
	out := make([]*Code, 0, count)
	for i := 0; i < count && r.err == nil; i++ {
		code := &Code{Name: r.str()}
		registers := r.u32()
		if registers > maxRegisters {
			r.fail("invalid register count %d", registers)
			break
		}
		code.Registers = int(registers)
		ops := r.count(maxInstructions)
		if ops > (len(r.buf)-r.off)/21 {
			r.fail("truncated instruction stream")
			break
		}
		code.Ops = make([]Op, 0, ops)
		for j := 0; j < ops && r.err == nil; j++ {
			op := Op{
				Code: OpCode(r.u8()),
				A:    r.i32(),
				B:    r.i32(),
				C:    r.i32(),
				D:    r.i32(),
				Stmt: r.i32(),
			}
			code.Ops = append(code.Ops, op)
		}
		out = append(out, code)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return out, nil
}

func readValue(r *reader, depth int) (any, error) {
	if depth > maxValueDepth {
		return nil, fmt.Errorf("value nesting exceeds %d levels", maxValueDepth)
	}
	tag := r.u8()
	if r.err != nil {
		return nil, r.err
	}
	var value any
	switch tag {
	case 0:
		value = nil
	case 1:
		value = false
	case 2:
		value = true
	case 3:
		value = syntax.Number(r.str())
	case 4:
		value = r.str()
	case 5:
		value = syntax.Temporal(r.str())
	case 6:
		count := r.count(maxConstants)
		array := make([]any, 0, count)
		for i := 0; i < count && r.err == nil; i++ {
			item, err := readValue(r, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		value = array
	case 7:
		count := r.count(maxConstants)
		object := make(map[string]any, count)
		var previous string
		for i := 0; i < count && r.err == nil; i++ {
			key := r.str()
			if i > 0 && key <= previous {
				return nil, fmt.Errorf("object keys are not strictly sorted")
			}
			previous = key
			item, err := readValue(r, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = item
		}
		value = object
	default:
		return nil, fmt.Errorf("unknown value tag %d", tag)
	}
	normalized, err := syntax.NormalizeValue(value)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}
