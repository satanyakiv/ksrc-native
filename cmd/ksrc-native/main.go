// Command ksrc-native searches and reads Kotlin/Native platform API signatures.
package main

import (
	"os"

	"github.com/satanyakiv/ksrc-native/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
