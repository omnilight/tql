package tql_test

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/vasileknik76/tql"
)

func TestParameterValues(t *testing.T) {
	for _, tt := range []struct {
		name, source, format, script, want string
		params                             map[string]any
	}{
		{"dynamic key", "# keep\n[services.api]\nimage='old' # image\nreplicas=1\n", "toml", `param $service; param $image; param $replicas = 2; services[$service].image = $image; services[$service].replicas = $replicas`, "# keep\n[services.api]\nimage=\"new\" # image\nreplicas=2\n", map[string]any{"service": "api", "image": "new"}},
		{"filter", "users:\n  - name: alice\n    enabled: false # keep\n", "yaml", `param $name; param $enabled; users[name == $name].enabled = $enabled`, "users:\n  - name: alice\n    enabled: true # keep\n", map[string]any{"name": "alice", "enabled": true}},
		{"exact number", `{"n":0}`, "json", `param $n; n = $n`, `{"n":9007199254740993}`, map[string]any{"n": uint64(9007199254740993)}},
		{"explicit null", `{"n":0}`, "json", `param $n = 3; n = $n`, `{"n":null}`, map[string]any{"n": nil}},
		{"false overrides", `{"n":0}`, "json", `param $n = true; n = $n`, `{"n":false}`, map[string]any{"n": false}},
		{"zero overrides", `{"n":9}`, "json", `param $n = 3; n = $n`, `{"n":0}`, map[string]any{"n": 0}},
		{"default object", `{}`, "json", `param $p = {v: [-2, +3, null, false, "x"]}; p = $p`, `{"p":{"v":[-2,3,null,false,"x"]}}`, nil},
		{"string is data", `{"n":0}`, "json", `param $n; n = $n`, `{"n":"\"; del($); #"}`, map[string]any{"n": `"; del($); #`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := tql.Parse([]byte(tt.source), tt.format)
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Exec(tt.script, tql.WithParams(tt.params))
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Bytes) != tt.want {
				t.Fatalf("got %s; want %s", result.Bytes, tt.want)
			}
		})
	}
}

func TestParameterErrorsRollback(t *testing.T) {
	for _, tt := range []struct {
		script     string
		params     map[string]any
		diagnostic string
	}{
		{`param $missing; n = 2`, nil, "missing required parameter $missing"},
		{`param $p = 1; n = 2`, map[string]any{"typo": 1}, `unknown parameter "typo"`},
		{`n = 2`, map[string]any{"p": 1}, `unknown parameter "p"`},
		{`param $p; n = 2; $p = {}`, map[string]any{"p": map[string]any{"x": 1}}, "parameter $p is read-only"},
		{`param $p; n = 2; $p.x = 1`, map[string]any{"p": map[string]any{"x": 1}}, "parameter $p is read-only"},
		{`param $p; n = 2; $p.new.field = 1`, map[string]any{"p": map[string]any{}}, "parameter $p is read-only"},
		{`param $p; n = 2; del($p)`, map[string]any{"p": []any{1}}, "parameter $p is read-only"},
		{`param $p; n = 2; del($p[0])`, map[string]any{"p": []any{1}}, "parameter $p is read-only"},
		{`param $p; n = 2; $p[1] = 2`, map[string]any{"p": []any{1}}, "parameter $p is read-only"},
		{`param $p; n = 2; for $p in $p[] { $p += 1 }`, map[string]any{"p": []any{1}}, "parameter $p is read-only"},
		{`param $p; n = 2; for $x in walk($p) { del($x) }`, map[string]any{"p": map[string]any{"x": 1}}, "parameter $p is read-only"},
		{`param $p; n = 2; for $x in $p[] { for $y in $x[] { $y = 9 } }`, map[string]any{"p": []any{[]any{1}}}, "parameter $p is read-only"},
		{`param $p = null; n = 2; $p ??= 1`, nil, "parameter $p is read-only"},
		{`param $p = 1; n = 2; let $p = 3`, nil, "already declared"},
		{`param $p; n = 2; n = 1 / $p`, map[string]any{"p": 0}, "division by zero"},
	} {
		t.Run(tt.script, func(t *testing.T) {
			source := []byte(`{"n":1}`)
			doc, err := tql.Parse(source, "json")
			if err != nil {
				t.Fatal(err)
			}
			result, err := doc.Exec(tt.script, tql.WithParams(tt.params))
			if err == nil || !strings.Contains(err.Error(), tt.diagnostic) {
				t.Fatalf("result=%v err=%v; want %q", result, err, tt.diagnostic)
			}
			if result != nil || !bytes.Equal(doc.Bytes(), source) {
				t.Fatal("failed execution committed partial results")
			}
		})
	}
}

func TestParameterSnapshotsAndScopes(t *testing.T) {
	input := map[string]any{"p": map[string]any{"items": []any{1, 2}}}
	option := tql.WithParams(input)
	input["p"].(map[string]any)["items"].([]any)[0] = 99
	delete(input, "p")
	script := `param $p; let $copy = $p; $copy.items[0] = 5; if true { let $p = 7; $p += 1; $p; } [$p, $copy]`
	doc, err := tql.Parse([]byte(`{}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r, err := doc.Exec(script, option)
		if err != nil {
			t.Fatal(err)
		}
		got, err := jsonValue(r.Values)
		if err != nil {
			t.Fatal(err)
		}
		if got != `[8,[{"items":[1,2]},{"items":[5,2]}]]` || r.Changed {
			t.Fatalf("values=%s changed=%v", got, r.Changed)
		}
		r.Values[1].([]any)[0].(map[string]any)["items"].([]any)[0] = 88
	}
	if _, err := doc.Query(`$p`); err == nil {
		t.Fatal("parameter leaked into next execution")
	}
}

func TestParameterProgramReuse(t *testing.T) {
	p, err := tql.Compile(`param $n; param $default = {items:[1]}; let $copy = $default; $copy.items[0] = $n; n = $copy.items[0]; $default`)
	if err != nil {
		t.Fatal(err)
	}
	shared := tql.WithParams(map[string]any{"n": 42})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				option := shared
				want := `{"n":42}`
				if i%2 == 0 {
					option = tql.WithParams(map[string]any{"n": i})
					want = `{"n":` + strconv.Itoa(i) + `}`
				}
				doc, err := tql.Parse([]byte(`{"n":0}`), "json")
				if err != nil {
					t.Error(err)
					return
				}
				r, err := doc.Execute(p, option)
				if err != nil {
					t.Error(err)
					return
				}
				defaults, err := jsonValue(r.Values[0])
				if err != nil || defaults != `{"items":[1]}` || string(r.Bytes) != want {
					t.Errorf("result=%s defaults=%s err=%v", r.Bytes, defaults, err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestParameterQueryAndEvalOptions(t *testing.T) {
	doc, err := tql.Parse([]byte(`{}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	values, err := doc.Query(`param $n; $n + 1`, tql.WithParams(map[string]any{"n": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] != tql.Number("3") {
		t.Fatalf("values=%v", values)
	}
	r, err := tql.Eval([]byte("old='before'\n"), "toml", `param $n; new = $n`, tql.WithStyle(tql.Style{Strings: "double"}), tql.WithParams(map[string]any{"n": "after"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Bytes) != "old='before'\nnew=\"after\"\n" {
		t.Fatalf("bytes=%s", r.Bytes)
	}
	if _, err := doc.Query(`param $n; n = $n`, tql.WithParams(map[string]any{"n": 1})); err == nil {
		t.Fatal("Query accepted mutation")
	}
	for _, options := range [][]tql.ExecOption{{nil}, {tql.WithParams(nil), tql.WithParams(nil)}} {
		if _, err := doc.Exec(`1`, options...); err == nil {
			t.Fatal("accepted invalid execution options")
		}
	}
	if _, err := tql.Eval([]byte(`{}`), "json", `1`, nil); err == nil {
		t.Fatal("accepted nil Eval option")
	}
	if _, err := tql.Eval([]byte(`{}`), "json", `1`, tql.WithParams(nil), tql.WithParams(nil)); err == nil {
		t.Fatal("accepted duplicate Eval parameters")
	}
}

func TestInvalidParameterInputs(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, params := range []map[string]any{
		{"$p": 1}, {"": 1}, {"true": 1}, {"a b": 1}, {"p": make(chan int)},
		{"p": math.Inf(1)}, {"p": math.NaN()}, {"p": cycle}, {"p": tql.Number("invalid")},
		{"p": string([]byte{0xff})}, {"p": []any{map[string]any{"bad": func() {}}}},
	} {
		if _, err := tql.Eval([]byte(`{}`), "json", `param $p = null; $p`, tql.WithParams(params)); err == nil {
			t.Fatalf("accepted invalid input of type %T", params["p"])
		}
	}
}
