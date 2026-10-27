package tql_test

import (
	"fmt"

	"github.com/vasileknik76/tql"
)

func ExampleWithParams() {
	program, err := tql.Compile(`param $port = 9090; server.port = $port`)
	if err != nil {
		panic(err)
	}
	for _, port := range []int{9090, 8081} {
		doc, err := tql.Parse([]byte("[server]\nport = 8080 # public\n"), "toml")
		if err != nil {
			panic(err)
		}
		result, err := doc.Execute(program, tql.WithParams(map[string]any{"port": port}))
		if err != nil {
			panic(err)
		}
		fmt.Print(string(result.Bytes))
	}
	// Output:
	// [server]
	// port = 9090 # public
	// [server]
	// port = 8081 # public
}
