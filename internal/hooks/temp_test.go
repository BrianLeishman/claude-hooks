package hooks

import "fmt"

var unusedVar int

func testFunction() {
	fmt.Println("test")
	undefinedVar := someUndefinedFunction()
	fmt.Println(undefinedVar)
}
