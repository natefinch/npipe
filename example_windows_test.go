package npipe_test

import (
	"bufio"
	"fmt"
	"github.com/natefinch/npipe"
)

// Use Dial to connect to a server and read messages from it.
func ExampleDial() {
	conn, err := npipe.Dial(`\\.\pipe\mypipe`)
	if err != nil {
		fmt.Println("Error from dial: ", err)
	}
	if _, err := fmt.Fprintln(conn, "Hi server!"); err != nil {
		fmt.Println("Error on client writing to pipe: ", err)
	}
	r := bufio.NewReader(conn)
	if msg, err := r.ReadString('\n'); err != nil {
		fmt.Println("Error on client reading from pipe: ", err)
	} else {
		fmt.Print(msg)
	}
}
