package tql_test

import (
	"fmt"

	"github.com/vasileknik76/tql"
)

func ExampleDocument_Exec() {
	doc, err := tql.Parse([]byte("# listener\nport  = 8080 # public\n"), "toml")
	if err != nil {
		panic(err)
	}
	result, err := doc.Exec(`port = 9090; port`)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(result.Bytes))
	fmt.Println(result.Values[0], result.Changed)
	// Output:
	// # listener
	// port  = 9090 # public
	// 9090 true
}
func ExampleCompile() {
	program, err := tql.Compile(`users[name == "alice"].enabled = true; users[0].enabled`)
	if err != nil {
		panic(err)
	}
	doc, err := tql.Parse([]byte(`{"users":[{"name":"alice","enabled":false}]}`), "json")
	if err != nil {
		panic(err)
	}
	result, err := doc.Execute(program)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(result.Bytes))
	fmt.Println(result.Values)
	// Output:
	// {"users":[{"name":"alice","enabled":true}]}
	// [true]
}
