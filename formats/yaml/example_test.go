package yaml_test

import (
	"fmt"

	"github.com/vasileknik76/tql/formats/yaml"
)

func ExampleParseStream() {
	stream, err := yaml.ParseStream([]byte("---\n? [a, b]\n: !label value\n---\n&node [*node]\n"))
	if err != nil {
		panic(err)
	}
	first := stream.Documents[0].Root
	fmt.Println(len(stream.Documents))
	fmt.Println(first.Content[0].Kind, first.Content[0].Content[0].Value)
	fmt.Println(first.Content[1].Tag, first.Content[1].Value)
	second := stream.Documents[1].Root
	fmt.Println(second.Content[0].Alias == second)
	// Output:
	// 2
	// sequence a
	// !label value
	// true
}
